# frozen_string_literal: true

module Toggleslash
  # Immutable view of what a client holds between syncs: the ruleset and the ETag that names it, swapped
  # together by one assignment so a reader never sees one without the other (contract 6.3).
  Held = Data.define(:ruleset, :etag)

  # What changes together under the state lock (contract 6.5).
  ClientState = Data.define(:last_sync_at, :stream, :last_error)

  # A ToggleSlash server SDK client. Build one with {Toggleslash.create}.
  #
  # Every public method is safe to call from any thread. None raises at the caller; `variation` and its
  # kin and `status` do no I/O.
  class Client
    DEFAULT_POLL = 30.0
    DEFAULT_FLUSH = 30.0
    DEFAULT_TIMEOUT = 10.0
    MAX_WAIT = 31_536_000.0
    INT64_MIN = -(2**63)
    INT64_MAX = (2**63) - 1
    FLOAT_INT64_LIMIT = 9_223_372_036_854_775_808.0

    # @api private The keywords after `on_error` are test seams, not part of the public API.
    def initialize(url:, sdk_key:, stream: true, poll_interval: DEFAULT_POLL, flush_interval: DEFAULT_FLUSH,
                   request_timeout: DEFAULT_TIMEOUT, hooks: nil, on_error: nil,
                   transport: nil, clock: nil, jitter: nil, stream_watchdog: nil, evaluator: nil)
      @endpoint = Http::Endpoint.new(url.is_a?(String) ? url.sub(%r{/+\z}, "") : "")
      @headers = { "Authorization" => "Bearer #{sdk_key if sdk_key.is_a?(String)}" }.freeze
      @stream_on = stream != false
      @poll_interval = Client.duration(poll_interval, DEFAULT_POLL)
      @flush_interval = Client.duration(flush_interval, DEFAULT_FLUSH)
      @request_timeout = Client.duration(request_timeout, DEFAULT_TIMEOUT)
      @after_evaluation = Client.hook_of(hooks)
      @on_error = on_error.respond_to?(:call) ? on_error : nil
      @transport = transport || Http::Transport.new
      @clock = clock || Clock.new
      @jitter = jitter || -> { 0.5 + (Random.rand * 0.5) }
      @watchdog = stream_watchdog || Http::STREAM_IDLE
      @evaluator = evaluator || Eval.method(:evaluate)
      setup_state
      @usage = Usage.new(endpoint: @endpoint, headers: @headers, transport: @transport, clock: @clock,
                         request_timeout: @request_timeout, report: method(:report_usage))
    end

    # Starts the two background threads and the first ruleset request. Returns the client.
    # @api private Use {Toggleslash.create}.
    def start
      @sync_thread = spawn_thread("toggleslash-sync") { run_sync }
      @flush_thread = spawn_thread("toggleslash-flush") { run_flush }
      self
    end

    # The client never prints its key.
    def inspect
      "#<#{self.class.name}>"
    end
    alias to_s inspect

    def self.duration(value, default)
      return default unless value.is_a?(Integer) || value.is_a?(Float) || value.is_a?(Rational)
      return default if value.is_a?(Float) && !value.finite?
      return default unless value.positive?

      value > MAX_WAIT ? MAX_WAIT : value.to_f
    end

    def self.hook_of(hooks)
      hook = if hooks.respond_to?(:after_evaluation)
               hooks.after_evaluation
             elsif hooks.is_a?(Hash)
               hooks[:after_evaluation] || hooks["after_evaluation"]
             end
      hook.respond_to?(:call) ? hook : nil
    end

    # ---------------------------------------------------------------- evaluation

    # The flag's value for `context`, or `fallback`. Never raises.
    def variation(flag_key, context, fallback)
      evaluate(flag_key, context, fallback, false).value
    end

    # Like {#variation}, with the variation index and the reason.
    def variation_detail(flag_key, context, fallback)
      evaluate(flag_key, context, fallback, false)
    end

    # The integer accessor: a fraction or a value beyond signed 64 bits is TYPE_MISMATCH.
    def int_variation(flag_key, context, fallback)
      evaluate(flag_key, context, fallback, true).value
    end

    def int_variation_detail(flag_key, context, fallback)
      evaluate(flag_key, context, fallback, true)
    end

    # ---------------------------------------------------------------- ready and status

    # Blocks until the first ruleset attempt has settled (bounded). True if a ruleset is held. Called from
    # one of the SDK's own threads it does not wait for itself.
    def ready
      return @ready_value if @ready_event.set?
      return !@held.ruleset.nil? if own_thread?

      @ready_event.wait(@request_timeout + 1.0)
      settle(!@held.ruleset.nil?)
      @ready_value
    end

    # One snapshot of the ETag, the last sync, the stream and the last error.
    def status
      held, state = @state_lock.synchronize { [@held, @state] }
      Status.new(held.etag, state.last_sync_at, state.stream, state.last_error)
    end

    # Stops, sends the final usage flush and returns. Safe twice, and from many threads.
    def close
      leader = @close_lock.synchronize do
        first = !@close_started
        @close_started = true
        first
      end
      unless leader
        @closed.wait(4 * @request_timeout) unless own_thread?
        return nil
      end

      stop_following
      if own_thread?
        # Contract 6.4: a close on the SDK's own thread does not wait for that thread's work. The rest of
        # shutdown, the final flush included, runs on a thread of its own, which ends when it is done.
        Thread.new { finish_close }.name = "toggleslash-close"
        return nil
      end
      finish_close
      nil
    end

    # @api private The JSON kind of a fallback, :any for nil, or :invalid for a class that maps to none.
    def self.kind_of(fallback)
      case fallback
      when nil then :any
      when true, false then :boolean
      when Integer, Float then :number
      when String then :string
      when Array then :array
      when Hash then :object
      else :invalid
      end
    end

    def self.served_matches?(kind, value)
      return true if kind == :any
      return false if value.nil? || kind == :invalid

      case kind
      when :boolean then [true, false].include?(value)
      when :number then value.is_a?(Integer) || value.is_a?(Float)
      when :string then value.is_a?(String)
      when :array then value.is_a?(Array)
      else value.is_a?(Hash)
      end
    end

    # The served number as an Integer, or nil when it has a fraction or leaves signed 64 bits.
    def self.as_int64(value)
      case value
      when Integer then value >= INT64_MIN && value <= INT64_MAX ? value : nil
      when Float
        return nil unless value.finite? && value >= -FLOAT_INT64_LIMIT && value < FLOAT_INT64_LIMIT

        value == value.floor ? value.to_i : nil
      end
    end

    private

    def setup_state
      @held = Held.new(nil, nil)
      @state_lock = Mutex.new
      @state = ClientState.new(nil, @stream_on ? StreamState::CONNECTING : StreamState::OFF, nil)
      @closing = Event.new
      @aborts = Http::Aborts.new
      @hold_until = 0.0
      @ready_event = Event.new
      @ready_value = false
      @close_lock = Mutex.new
      @close_started = false
      @closed = Event.new
    end

    def spawn_thread(name)
      Thread.new do
        Thread.current.name = name
        Thread.current.report_on_exception = false
        begin
          yield
        rescue StandardError, SystemStackError
          nil
        end
      end
    end

    def own_thread?
      me = Thread.current
      [@sync_thread, @flush_thread].include?(me)
    end

    # ---------------------------------------------------------------- callbacks

    # Hands an error to `on_error`. No SDK lock is held; whatever it raises is dropped.
    def notify(error)
      callback = @on_error
      return unless callback

      begin
        callback.call(error)
      rescue StandardError, ScriptError
        nil
      end
    end

    def report_usage(code, message)
      notify(Error.new(message, code))
    end

    # ---------------------------------------------------------------- the evaluation path

    def evaluate(flag_key, context, fallback, integer)
      held = @held
      failure = nil
      begin
        detail = compute(held, flag_key, context, fallback, integer)
      rescue StandardError, SystemStackError => e
        failure = e
        detail = error_detail(fallback, ErrorCode::EVALUATION_FAILED)
      end
      notify(Error.new("evaluation failed: #{failure.class.name}", ErrorCode::EVALUATION_FAILED)) if failure
      run_hook(held, flag_key, context, detail)
      detail
    end

    def error_detail(fallback, code)
      Detail.new(fallback, nil, Reason.new(kind: ReasonKind::ERROR, code: code))
    end

    def compute(held, flag_key, context, fallback, integer)
      ruleset = held.ruleset
      return error_detail(fallback, ErrorCode::NOT_READY) if ruleset.nil?

      value, index, reason = @evaluator.call(ruleset, flag_key, context)
      return Detail.new(fallback, nil, reason) if index.nil?

      kind = Client.kind_of(fallback)
      if integer
        # A nil fallback says nothing about the kind: the accessor's own kind (a number) is accepted.
        return error_detail(fallback, ErrorCode::TYPE_MISMATCH) unless %i[number any].include?(kind)

        whole = Client.as_int64(value)
        return error_detail(fallback, ErrorCode::TYPE_MISMATCH) if whole.nil?

        value = whole
      elsif !Client.served_matches?(kind, value)
        return error_detail(fallback, ErrorCode::TYPE_MISMATCH)
      end
      @usage.add(flag_key, index, @clock.now.getutc.strftime("%Y-%m-%d"))
      Detail.new(value, index, reason)
    end

    def run_hook(held, flag_key, context, detail)
      hook = @after_evaluation
      return unless hook

      version = begin
        held.ruleset&.flag_version(flag_key)
      rescue StandardError
        nil
      end
      begin
        hook.call(EvaluationEvent.new(flag_key, context, detail.value, detail.variation_index, detail.reason,
                                      held.etag, version))
      rescue StandardError, ScriptError => e
        notify(e)
      end
    end

    # ---------------------------------------------------------------- state changes

    def settle(value)
      @state_lock.synchronize do
        return if @ready_event.set?

        @ready_value = value
        @ready_event.set
      end
    end

    def change_stream(stream)
      @state_lock.synchronize do
        return if @state.stream == StreamState::CLOSED

        @state = @state.with(stream: stream)
      end
    end

    # Stream `connected` and the cleared error change in one step.
    def connected!
      @state_lock.synchronize do
        return if @state.stream == StreamState::CLOSED

        @state = @state.with(stream: StreamState::CONNECTED, last_error: nil)
      end
    end

    def synced(held)
      now = @clock.now.getutc
      @state_lock.synchronize do
        @held = held if held
        @state = @state.with(last_sync_at: now, last_error: nil)
      end
    end

    # A failure of a ruleset or stream request. Nothing is set for a failure `close` caused. A failure that
    # ends the stream sets the stream state with the error, in one step.
    def sync_error(code, message, stream = nil)
      @state_lock.synchronize do
        return if @closing.set?

        @state = @state.with(last_error: code)
        @state = @state.with(stream: stream) if stream
      end
      notify(Error.new(message, code))
    end

    # ---------------------------------------------------------------- holds

    def hold(seconds)
      until_time = @clock.monotonic + seconds
      @state_lock.synchronize { @hold_until = [@hold_until, until_time].max }
    end

    def wait_hold
      until @closing.set?
        remaining = @state_lock.synchronize { @hold_until } - @clock.monotonic
        return if remaining <= 0
        return if @clock.wait(@closing, remaining)
      end
    end

    # ---------------------------------------------------------------- the sync thread

    def run_sync
      begin
        fetch_ruleset
      rescue StandardError
        nil
      ensure
        settle(!@held.ruleset.nil?)
      end
      sync_loop
    end

    def sync_loop
      failures = 0
      until @closing.set?
        if @stream_on
          wait_hold
          return if @closing.set?
        end
        connected = @stream_on ? follow : false
        return if @closing.set?

        failures = connected ? 0 : failures + 1
        return if @clock.wait(@closing, delay(failures))

        wait_hold
        return if @closing.set?

        fetch_ruleset unless connected
      end
    end

    def delay(failures)
      return @poll_interval unless @stream_on

      [@poll_interval, 2.0**[failures, 16].min].min * @jitter.call
    end

    # The ruleset request of 7.1. True when it was a sync. `stream` is the state to set with an error that
    # ends a stream.
    def fetch_ruleset(stream = nil)
      headers = { "Accept" => "application/json" }.merge(@headers)
      held = @held
      headers["If-None-Match"] = held.etag if held.ruleset && held.etag
      response = request_ruleset(headers, stream)
      return false unless response

      status = response.status
      return accept(response, stream) if status == 200

      if status == 304 && held.ruleset
        synced(nil)
        return true
      end
      hold_for(response) if status == 429
      code = LastError.http(status)
      sync_error(code, "ruleset request failed: #{code}", stream)
      false
    end

    def request_ruleset(headers, stream)
      @transport.fetch(@endpoint, "GET", "/sdk/v1/ruleset", headers, nil, @request_timeout, @aborts)
    rescue Http::RequestTimeout
      sync_error(LastError::TIMEOUT, "ruleset request failed: timeout", stream)
      nil
    rescue Http::Aborted
      nil
    rescue StandardError
      sync_error(LastError::NETWORK, "ruleset request failed: network", stream)
      nil
    end

    def hold_for(response)
      seconds = Usage.retry_after(response)
      hold(seconds) if seconds
    end

    def accept(response, stream)
      ruleset = parse_ruleset(response.body)
      if ruleset.nil?
        sync_error(LastError::BAD_RULESET, "ruleset request failed: bad_ruleset", stream)
        return false
      end
      etag = response.header("etag")
      synced(Held.new(ruleset, etag.nil? || etag.empty? ? nil : etag))
      true
    end

    def parse_ruleset(body)
      raw = Json.parse(body)
      return nil unless raw.is_a?(Hash) && raw["flags"].is_a?(Array) && raw["segments"].is_a?(Array)

      Eval.load_ruleset(raw)
    rescue StandardError, SystemStackError
      nil
    end

    # ---------------------------------------------------------------- the stream

    # One stream attempt (7.2). True when it connected and was not ended by a failed fetch.
    def follow
      change_stream(StreamState::CONNECTING)
      stream = nil
      begin
        stream = open_stream
        return false unless stream

        if stream.status != 200 || stream.bodyless?
          hold_for(stream) if stream.status == 429
          code = LastError.http(stream.status)
          sync_error(code, "stream request failed: #{code}", StreamState::DISCONNECTED)
          return false
        end
        read_events(stream)
      ensure
        stream&.close
        change_stream(StreamState::DISCONNECTED)
      end
    end

    def open_stream
      headers = { "Accept" => "text/event-stream" }.merge(@headers)
      @transport.open_stream(@endpoint, "/sdk/v1/stream", headers, @request_timeout, @watchdog, @aborts)
    rescue Http::RequestTimeout
      sync_error(LastError::TIMEOUT, "stream request failed: timeout", StreamState::DISCONNECTED)
      nil
    rescue Http::Aborted
      nil
    rescue StandardError
      sync_error(LastError::NETWORK, "stream request failed: network", StreamState::DISCONNECTED)
      nil
    end

    def read_events(stream)
      parser = SseParser.new
      connected = false
      loop do
        chunk = read_chunk(stream)
        return connected unless chunk

        parser.feed(chunk).each do |name, data|
          if name == "connected"
            connected = true
            connected!
          elsif name != "changed"
            next
          end
          return connected if @closing.set?
          return false unless refresh(data)
        end
      end
    end

    # The next bytes, nil at a clean end, or false after a failure that has been recorded.
    def read_chunk(stream)
      stream.read
    rescue Http::RequestTimeout
      sync_error(LastError::TIMEOUT, "stream failed: timeout", StreamState::DISCONNECTED)
      false
    rescue Http::Aborted
      false
    rescue StandardError
      sync_error(LastError::NETWORK, "stream failed: network", StreamState::DISCONNECTED)
      false
    end

    # A stream event named a tag: a sync with no fetch when it is the held one, else the ruleset request.
    def refresh(tag)
      held = @held
      if held.ruleset && held.etag && held.etag == tag
        synced(nil)
        true
      else
        fetch_ruleset(StreamState::DISCONNECTED)
      end
    end

    # ---------------------------------------------------------------- usage and close

    def run_flush
      until @clock.wait(@closing, @flush_interval)
        begin
          @usage.flush
        rescue StandardError
          nil
        end
      end
    end

    def stop_following
      @state_lock.synchronize do
        @closing.set
        @state = @state.with(stream: StreamState::CLOSED)
      end
      @aborts.abort_all
    end

    def finish_close
      shut_down
    ensure
      @aborts.dispose
      @closed.set
    end

    def shut_down
      deadline = Clock.monotonic + @request_timeout
      me = Thread.current
      [@sync_thread, @flush_thread].each do |thread|
        next if thread.nil? || thread == me || !thread.alive?

        thread.join([deadline - Clock.monotonic, 0.0].max)
      end
      @usage.stop
      final_flush
      settle(false)
    end

    def final_flush
      begin
        @usage.flush(@request_timeout)
      rescue StandardError
        nil
      end
      pending = @usage.pending
      return unless pending.positive? && @usage.holding?

      notify(Error.new("closed with #{pending} usage counts unsent: the server asked to wait", "usage_unsent"))
    end
  end
end
