# frozen_string_literal: true

require_relative 'config'
require_relative 'http'
require_relative 'json'
require_relative 'state'

module ToggleSlash
  # Counts how often each flag variation was served, per UTC day, and sends the totals in batches
  # (contract section 9). `record` is a hash add under a lock held for nothing else, so it is safe on a
  # request path and never waits for a flush. A failure is handed to `report` after the flush lock has been
  # let go, so a callback that calls back into the client cannot deadlock it.
  class UsageReporter
    # The server takes 1000 events and 128 KiB a request; an entry is at most about 140 bytes, so 500 stay
    # under both.
    MAX_BATCH = 500

    Entry = Struct.new(:flag, :variation_index, :day, :count)

    def initialize(config, hold, report)
      @config = config
      @hold = hold
      @report = report
      @lock = Mutex.new
      @flush_lock = Mutex.new
      @pending = {}
      @counting = true
    end

    def record(flag, variation_index)
      day = @config.clock.now.getutc.strftime('%Y-%m-%d')
      @lock.synchronize do
        return unless @counting

        add(Entry.new(flag.dup.freeze, variation_index, day, 1))
      end
    end

    # Nothing is counted from now on.
    def stop_counting
      @lock.synchronize { @counting = false }
    end

    # Sends what is pending. A call during another flush waits for it first. Each request is bounded by
    # `request_timeout`, and `close` waits for a flush that is running (contract 11, step 3).
    def flush
      errors = []
      @flush_lock.synchronize { send_pending(errors) }
      errors.each { |error| @report.call(error) }
    end

    # The last flush, on closing. It does not wait out a Retry-After and does not ignore one: counts still
    # held back then are lost, and that is reported.
    def finish
      flush
      unsent = @lock.synchronize { @pending.size }
      return unless unsent.positive? && @hold.holding?

      @report.call(StandardError.new("closed with #{unsent} usage counts unsent: the server asked to wait"))
    end

    private

    # Merges an entry into the pending map. The caller holds @lock.
    def add(entry)
      key = [entry.flag, entry.variation_index, entry.day]
      existing = @pending[key]
      if existing
        existing.count += entry.count
      else
        @pending[key] = entry
      end
    end

    def send_pending(errors)
      return if @hold.holding?

      entries = @lock.synchronize do
        taken = @pending.values
        @pending = {}
        taken
      end
      start = 0
      while start < entries.length
        unless post(entries[start, MAX_BATCH], errors)
          @lock.synchronize { entries[start..].each { |entry| add(entry) } }
          return
        end
        start += MAX_BATCH
      end
    end

    # What to do with a batch the server did not take: try it again later, or give up on it.
    def retryable?(status)
      status >= 500 || status == 408 || status == 429
    end

    # False when the batch should be kept for another try.
    def post(batch, errors)
      body = Json.generate('events' => batch.map { |entry| wire(entry) })
      response = Http.request(@config.target, 'POST', '/sdk/v1/events',
                              deadline: Clock.monotonic + @config.request_timeout, body: body,
                              headers: { 'Authorization' => "Bearer #{@config.sdk_key}",
                                         'Content-Type' => 'application/json' })
      begin
        status = response.status
        retry_header = response.header('retry-after')
      ensure
        response.close
      end
      return true if status.between?(200, 299)

      @hold.extend_by(Http.retry_after(status, retry_header))
      errors << StandardError.new("POST /sdk/v1/events answered #{status}")
      !retryable?(status)
    rescue *RECOVERABLE => e
      errors << e
      false
    end

    def wire(entry)
      { 'flag' => entry.flag, 'variationIndex' => entry.variation_index, 'count' => entry.count, 'day' => entry.day }
    end
  end
end
