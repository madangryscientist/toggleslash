# frozen_string_literal: true

require_relative 'eval'
require_relative 'config'
require_relative 'http'
require_relative 'json'
require_relative 'sse'
require_relative 'state'

module ToggleSlash
  # Keeps the ruleset current: one conditional fetch at the start, then the stream when it can be had and
  # polls when it cannot (contract section 7). It runs on one thread. Every failure is recorded in the store
  # and handed to `report`; nothing is raised out of `run`, and the last good ruleset stays in place.
  class Syncer
    # A server answer that was not the expected one.
    class StatusError < StandardError
      attr_reader :status

      def initialize(status, route)
        @status = status
        super("GET #{route} answered #{status}")
      end
    end

    # A 200 whose body is not a ruleset.
    class BadRuleset < StandardError; end

    BACKOFF_BASE = 1.0
    BOM = "\xEF\xBB\xBF".b.freeze

    # `on_first_load` is called once the first attempt has ended, whatever came of it.
    def initialize(config, store, cancel, hold, report, on_first_load)
      @config = config
      @store = store
      @cancel = cancel
      @hold = hold
      @report = report
      @on_first_load = on_first_load
    end

    def run
      refresh
      @on_first_load.call
      failures = 0
      failures = step(failures) until @cancel.cancelled?
    ensure
      @on_first_load.call
    end

    private

    # One turn of the loop of contract 7.3; returns the new failure count.
    def step(failures)
      wait_for(0) if @config.stream
      return failures if @cancel.cancelled?

      connected = @config.stream ? follow : false
      failures = connected ? 0 : failures + 1
      wait_for(delay(failures))
      refresh if !connected && !@cancel.cancelled?
      failures
    rescue *RECOVERABLE => e
      # A bug in the SDK itself. Say so, and slow down rather than spin.
      record(e) unless @cancel.cancelled?
      @cancel.wait([@config.poll_interval, 1.0].min)
      failures + 1
    end

    # Waits `seconds`, or until the Retry-After hold has passed if that is later.
    def wait_for(seconds)
      wait = [seconds, @hold.remaining].max
      @cancel.wait(wait) if wait.positive?
    end

    def delay(failures)
      return @config.poll_interval unless @config.stream

      ceiling = [@config.poll_interval, BACKOFF_BASE * (2**[failures, 16].min)].min
      ceiling * (0.5 + (@config.random.call / 2))
    end

    def deadline
      Clock.monotonic + @config.request_timeout
    end

    def headers(extra = {})
      { 'Authorization' => "Bearer #{@config.sdk_key}" }.merge(extra)
    end

    # One conditional fetch of the ruleset. True when it ended in a sync.
    def refresh
      state = @store.state
      extra = {}
      extra['If-None-Match'] = state.etag if state.etag && state.ruleset
      limit = deadline
      response = Http.request(@config.target, 'GET', '/sdk/v1/ruleset', deadline: limit, cancel: @cancel,
                                                                        headers: headers(extra))
      begin
        accept(response, limit)
      ensure
        response.close
      end
      true
    rescue *RECOVERABLE => e
      record(e) unless @cancel.cancelled?
      false
    end

    def accept(response, limit)
      case response.status
      when 304 then @store.synced(@config.clock.now)
      when 200 then load_ruleset(response, limit)
      else
        @hold.extend_by(Http.retry_after(response.status, response.header('retry-after')))
        raise StatusError.new(response.status, '/sdk/v1/ruleset')
      end
    end

    def load_ruleset(response, limit)
      body = response.read_body(limit)
      body = body.byteslice(BOM.bytesize..) if body.start_with?(BOM)
      parsed = Json.parse(body)
      unless parsed.is_a?(Hash) && parsed['flags'].is_a?(Array) && parsed['segments'].is_a?(Array)
        raise BadRuleset, 'the answer has no flags and segments'
      end

      ruleset = Eval::Rules.build(Json.freeze_deep(parsed))
      @store.swap(ruleset, response.header('etag'), @config.clock.now)
    end

    # Follows the stream until it ends or fails. True if it connected, so the loop can tell a stream that
    # was up from one that never was.
    def follow
      connected = false
      response = nil
      @store.stream = :connecting
      begin
        response = open_stream
        parser = Sse::Parser.new
        while (chunk = response.read_chunk(idle: @config.idle_timeout))
          parser.feed(chunk).each do |event|
            next unless %w[changed connected].include?(event.name)

            if event.name == 'connected'
              connected = true
              @store.connected
            end
            # The fetch the stream asked for failed, and a stream that stays up would never retry it:
            # end it and count it as not connected, so the backoff and the poll take over.
            return false unless on_tag(event.data)
          end
        end
      rescue *RECOVERABLE => e
        record(e) unless @cancel.cancelled?
      ensure
        response&.close
        @store.stream = :disconnected
      end
      connected
    end

    def stream_headers
      headers('Accept' => 'text/event-stream')
    end

    def open_stream
      response = Http.request(@config.target, 'GET', '/sdk/v1/stream', deadline: deadline, cancel: @cancel,
                                                                       headers: stream_headers)
      if response.status != 200 || response.bodiless?
        @hold.extend_by(Http.retry_after(response.status, response.header('retry-after')))
        response.close
        raise StatusError.new(response.status, '/sdk/v1/stream')
      end
      response
    end

    # The stream names the ruleset's ETag; fetch only when it is not the one held.
    def on_tag(tag)
      state = @store.state
      if state.ruleset && tag == state.etag
        @store.synced(@config.clock.now)
        return true
      end
      refresh
    end

    def record(error)
      @store.failed(code_for(error))
      @report.call(error)
    end

    def code_for(error)
      case error
      when StatusError then :"http_#{error.status}"
      when Http::Timeout then :timeout
      when BadRuleset, Json::ParseError then :bad_ruleset
      else :network
      end
    end
  end
end
