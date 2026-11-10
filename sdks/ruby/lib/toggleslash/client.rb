# frozen_string_literal: true

require_relative 'cancel'
require_relative 'clock'
require_relative 'config'
require_relative 'eval'
require_relative 'http'
require_relative 'state'
require_relative 'sync'
require_relative 'usage'

module ToggleSlash
  Detail = Eval::Detail
  Reason = Eval::Reason

  # What `after_evaluation` receives. `flag_version` is nil when the ruleset has no such flag or the flag
  # is malformed; `etag` is the ETag of the very ruleset this evaluation used.
  EvaluationEvent = Data.define(:flag_key, :context, :value, :variation_index, :reason, :etag, :flag_version)

  # A server SDK client (docs/sdk-contract.md). Thread safe: every method may be called from any thread, at
  # any time, `close` included. Nothing here raises at a caller.
  class Client
    INT64_MIN = -(2**63)
    INT64_MAX = (2**63) - 1

    # What a typed accessor asks of the served value: a JSON kind, and for `int` the integer rule of
    # contract section 4. A kind of nil accepts any value.
    Accessor = Struct.new(:kind, :integer)

    BOOL = Accessor.new(:boolean, false).freeze
    STRING = Accessor.new(:string, false).freeze
    NUMBER = Accessor.new(:number, false).freeze
    INT = Accessor.new(:number, true).freeze
    JSON_VALUE = Accessor.new(:json, false).freeze
    CONTAINER = Accessor.new(:container, false).freeze

    # `url:` and `sdk_key:` are required. The rest are in the table in README.md. Unknown keywords are
    # ignored, and so are values of the wrong type (they take the default).
    def initialize(url: nil, sdk_key: nil, stream: true, poll_interval: nil, flush_interval: nil,
                   request_timeout: nil, hooks: nil, on_error: nil, clock: Clock, random: nil,
                   evaluator: Eval::Evaluator, stream_idle_timeout: nil, **)
      @on_error = on_error.respond_to?(:call) ? on_error : nil
      @hook = self.class.read_hook(hooks)
      @evaluator = evaluator
      stream_on = stream != false
      @config = Config.new(
        target: Http.target(ToggleSlash.base_url(url)), sdk_key: sdk_key.is_a?(String) ? sdk_key : '',
        stream: stream_on, poll_interval: ToggleSlash.duration(poll_interval, DEFAULT_INTERVAL),
        flush_interval: ToggleSlash.duration(flush_interval, DEFAULT_INTERVAL),
        request_timeout: ToggleSlash.duration(request_timeout, DEFAULT_TIMEOUT),
        idle_timeout: ToggleSlash.duration(stream_idle_timeout, DEFAULT_IDLE_TIMEOUT),
        clock: clock, random: random || -> { Random.rand }
      )
      build_parts(stream_on)
      start
    end

    # Generic accessors: the served value must be of the fallback's JSON kind (a nil fallback accepts any).
    def variation(flag_key, context, fallback)
      variation_detail(flag_key, context, fallback).value
    end

    def variation_detail(flag_key, context, fallback)
      evaluate(flag_key, context, fallback, Accessor.new(kind_of(fallback), false))
    end

    {
      bool: BOOL, string: STRING, number: NUMBER, int: INT, json: JSON_VALUE
    }.each do |name, accessor|
      define_method(:"#{name}_variation") do |flag_key, context, fallback|
        evaluate(flag_key, context, fallback, accessor).value
      end
      define_method(:"#{name}_variation_detail") do |flag_key, context, fallback|
        evaluate(flag_key, context, fallback, accessor)
      end
    end

    # Waits for the first load to end and says whether a ruleset is now held. The answer is fixed then.
    def ready
      @ready.wait
    end

    def status
      @store.status
    end

    # Stops both threads, waits for them, sends the final usage counts. Safe to call twice and from many
    # threads at once; every call returns when the one shutdown is done.
    def close
      return nil unless begin_close

      begin
        shut_down
      ensure
        finish_close
      end
      nil
    end

    # Seconds still held back by a Retry-After on `route` (:sync or :usage). For tests.
    # @api private
    def hold_remaining(route)
      (route == :usage ? @usage_hold : @sync_hold).remaining
    end

    # Reads the hooks option: a Hash with :after_evaluation, or an object that responds to it.
    # @api private
    def self.read_hook(hooks)
      hook = if hooks.is_a?(Hash)
               hooks[:after_evaluation] || hooks['after_evaluation']
             elsif hooks.respond_to?(:after_evaluation)
               hooks.method(:after_evaluation)
             end
      hook.respond_to?(:call) ? hook : nil
    rescue *RECOVERABLE
      nil
    end

    private

    def build_parts(stream_on)
      @store = Store.new(stream_on)
      @sync_hold = Hold.new(@config.clock)
      @usage_hold = Hold.new(@config.clock)
      @ready = Latch.new
      @sync_cancel = Cancel.new
      @flush_cancel = Cancel.new
      report = method(:report)
      @usage = UsageReporter.new(@config, @usage_hold, report)
      @syncer = Syncer.new(@config, @store, @sync_cancel, @sync_hold, report,
                           -> { @ready.settle(!@store.state.ruleset.nil?) })
      @close_lock = Mutex.new
      @close_condition = ConditionVariable.new
      @close_state = :open
    end

    def start
      @sync_thread = spawn('toggleslash-sync') { @syncer.run }
      @flush_thread = spawn('toggleslash-flush') { flush_loop }
    end

    def spawn(name, &block)
      Thread.new do
        Thread.current.name = name
        Thread.current.report_on_exception = false
        block.call
      end
    end

    def flush_loop
      @usage.flush until @flush_cancel.wait(@config.flush_interval)
    end

    # Tells the application about a failure. An application callback that raises is ignored.
    def report(error)
      @on_error&.call(error)
      nil
    rescue *RECOVERABLE
      nil
    end

    # True for the one caller that does the shutdown; the others wait for it. A call from the closing thread
    # itself, or from one of the two SDK threads (an `on_error` that calls `close`), cannot wait for a shutdown
    # that is waiting for it, so it returns at once.
    def begin_close
      @close_lock.synchronize do
        if @close_state == :open
          @close_state = :closing
          @closer = Thread.current
          return true
        end
        return false if [@closer, @sync_thread, @flush_thread].include?(Thread.current)

        @close_condition.wait(@close_lock) until @close_state == :done
        false
      end
    end

    def finish_close
      @close_lock.synchronize do
        @close_state = :done
        @close_condition.broadcast
      end
    end

    def shut_down
      @usage.stop_counting
      @store.close
      @sync_cancel.cancel
      @flush_cancel.cancel
      [@sync_thread, @flush_thread].each { |thread| thread.join unless thread == Thread.current }
      @ready.settle(false)
      @usage.finish
      @sync_cancel.release
      @flush_cancel.release
    rescue *RECOVERABLE => e
      report(e)
    end

    def evaluate(flag_key, context, fallback, accessor)
      accessor = json_accessor(fallback) if accessor.equal?(JSON_VALUE)
      state = @store.state
      detail = answer(state.ruleset, flag_key, context, fallback, accessor)
      run_hook(state, flag_key, context, detail) unless @hook.nil?
      detail
    end

    def answer(ruleset, flag_key, context, fallback, accessor)
      return failure(fallback, :NOT_READY) if ruleset.nil?

      result = @evaluator.evaluate(ruleset, flag_key, context)
      return Detail.new(value: fallback, variation_index: nil, reason: result.reason) if result.variation_index.nil?

      matches, value = check(result.value, accessor)
      return failure(fallback, :TYPE_MISMATCH) unless matches

      @usage.record(flag_key, result.variation_index)
      Detail.new(value: value, variation_index: result.variation_index, reason: result.reason)
    rescue *RECOVERABLE => e
      report(e)
      failure(fallback, :EVALUATION_FAILED)
    end

    # The json accessor takes an array or an object, whichever the fallback is; with a nil fallback, either.
    def json_accessor(fallback)
      return CONTAINER if fallback.nil?

      Accessor.new(kind_of(fallback), false)
    end

    def failure(fallback, code)
      Detail.new(value: fallback, variation_index: nil, reason: Eval.error_reason(code))
    end

    def run_hook(state, flag_key, context, detail)
      event = EvaluationEvent.new(flag_key: flag_key, context: context, value: detail.value,
                                  variation_index: detail.variation_index, reason: detail.reason,
                                  etag: state.etag, flag_version: version_of(state.ruleset, flag_key))
      @hook.call(event)
    rescue *RECOVERABLE => e
      report(e)
    end

    # The flag's version, when the ruleset has the flag and it validated far enough to carry one.
    def version_of(ruleset, flag_key)
      entry = ruleset&.flags&.[](flag_key)
      entry && entry.status == :ready ? entry.flag['version'] : nil
    end

    def kind_of(value)
      case value
      when nil then nil
      when true, false then :boolean
      when Integer, Float then :number
      when String then :string
      when Array, Hash then value.is_a?(Array) ? :array : :object
      else :other
      end
    end

    # [whether the served value suits the accessor, the value to hand over].
    def check(served, accessor)
      return [true, served] if accessor.kind.nil?

      served_kind = kind_of(served)
      return [%i[array object].include?(served_kind), served] if accessor.kind == :container

      return [false, nil] unless served_kind == accessor.kind
      return [true, served] unless accessor.integer

      whole_number(served)
    end

    # The integer rule of contract section 4: no fraction, and inside signed 64 bits. 2.0 is 2.
    def whole_number(number)
      return [number.between?(INT64_MIN, INT64_MAX), number] if number.is_a?(Integer)
      return [false, nil] unless number.finite? && number == number.floor
      return [false, nil] unless number >= INT64_MIN.to_f && number < INT64_MAX.to_f + 1

      [true, number.to_i]
    end
  end

  # Builds a client and starts its two threads. It does not wait for the network and it does not raise.
  def self.create(**)
    Client.new(**)
  end
end
