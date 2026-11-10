# frozen_string_literal: true

module ToggleSlash
  # What `Client#status` reports: one consistent snapshot, never a live view.
  # `stream` is :off, :connecting, :connected, :disconnected or :closed. `last_error` is nil or :timeout,
  # :bad_ruleset, :network or :http_<status>. `last_sync_at` is a Time.
  Status = Data.define(:etag, :last_sync_at, :stream, :last_error)

  # What the SDK knows: the ruleset and the ETag that names it travel together in one frozen value, so a
  # reader sees the old pair or the new one and never a mix.
  State = Data.define(:ruleset, :etag, :last_sync_at, :stream, :last_error)

  # The one place the state lives. A swap replaces a reference under a lock held for nothing else.
  class Store
    def initialize(stream_on)
      @lock = Mutex.new
      @state = State.new(ruleset: nil, etag: nil, last_sync_at: nil, stream: stream_on ? :connecting : :off,
                         last_error: nil)
    end

    def state
      @lock.synchronize { @state }
    end

    def status
      current = state
      Status.new(etag: current.etag, last_sync_at: current.last_sync_at, stream: current.stream,
                 last_error: current.last_error)
    end

    # A ruleset replaces the one held, together with its ETag, and counts as a sync.
    def swap(ruleset, etag, now)
      update { |current| current.with(ruleset: ruleset, etag: etag, last_sync_at: now, last_error: nil) }
    end

    def synced(now)
      update { |current| current.with(last_sync_at: now, last_error: nil) }
    end

    def failed(code)
      update { |current| current.with(last_error: code) }
    end

    def connected
      update { |current| current.stream == :closed ? current : current.with(stream: :connected, last_error: nil) }
    end

    # Once closed, the stream stays :closed whatever an abandoned request does afterwards.
    def stream=(value)
      update { |current| current.stream == :closed ? current : current.with(stream: value) }
    end

    def close
      update { |current| current.with(stream: :closed) }
    end

    private

    def update
      @lock.synchronize do
        @state = yield(@state)
      end
    end
  end

  # A time before which nothing may be sent, set by a 429's Retry-After. A later hold lengthens it and
  # never shortens it.
  class Hold
    def initialize(clock)
      @clock = clock
      @lock = Mutex.new
      @until = 0.0
    end

    def extend_by(seconds)
      return if seconds.nil?

      @lock.synchronize { @until = [@until, @clock.monotonic + seconds].max }
    end

    # Seconds still to wait; 0 when none.
    def remaining
      @lock.synchronize { [@until - @clock.monotonic, 0.0].max }
    end

    def holding?
      remaining.positive?
    end
  end

  # One value that is set once and waited on, for `ready`.
  class Latch
    def initialize
      @lock = Mutex.new
      @condition = ConditionVariable.new
      @settled = false
      @value = false
    end

    def settle(value)
      @lock.synchronize do
        return if @settled

        @settled = true
        @value = value
        @condition.broadcast
      end
    end

    def wait
      @lock.synchronize do
        @condition.wait(@lock) until @settled
        @value
      end
    end
  end
end
