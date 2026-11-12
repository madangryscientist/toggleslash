# frozen_string_literal: true

module Toggleslash
  # Usage counts: (flag, variation index, UTC day) to a count, flushed in batches (contract 9).
  class Usage
    MAX_BATCH = 500
    MAX_HOLD = 300.0

    # The seconds a `Retry-After` asks for, capped at 300, or nil when the header is not whole decimal
    # digits (spaces around it are ignored).
    def self.retry_after(response)
      raw = response.header("retry-after")
      return nil unless raw.is_a?(String)

      text = raw.gsub(/\A +| +\z/, "")
      return nil unless text.match?(/\A[0-9]+\z/)
      return MAX_HOLD if text.length > 6

      [text.to_i.to_f, MAX_HOLD].min
    end

    def self.retryable?(status)
      status >= 500 || status == 408 || status == 429
    end

    def initialize(endpoint:, headers:, transport:, clock:, request_timeout:, report:)
      @endpoint = endpoint
      @headers = headers
      @transport = transport
      @clock = clock
      @request_timeout = request_timeout
      @report = report
      @lock = Mutex.new
      @flush_lock = Mutex.new
      @counts = {}
      @stopped = false
      @hold_until = 0.0
    end

    def add(flag, index, day)
      @lock.synchronize do
        return if @stopped

        key = [flag, index, day]
        count = @counts[key]
        if count
          @counts[key] = count + 1
        else
          @counts[[flag.dup.freeze, index, day]] = 1
        end
      end
    end

    # Nothing more is counted (called by `close` before the final flush).
    def stop
      @lock.synchronize { @stopped = true }
    end

    # The number of entries waiting to be sent.
    def pending
      @lock.synchronize { @counts.size }
    end

    def holding?
      @clock.monotonic < @lock.synchronize { @hold_until }
    end

    # Sends what is pending, in batches of at most 500, one request at a time. `lock_timeout` bounds the wait
    # for a flush already running (`close` passes one); nil waits as long as it takes.
    def flush(lock_timeout = nil)
      return unless acquire(lock_timeout)

      reports = []
      begin
        send_pending(reports)
      ensure
        @flush_lock.unlock
      end
      # The callbacks run with no lock held (contract 6.4).
      reports.each { |code, message| @report.call(code, message) }
    end

    private

    def send_pending(reports)
      return if holding?

      taken = @lock.synchronize do
        entries = @counts.to_a
        @counts = {}
        entries
      end
      send_all(taken, reports)
    end

    def acquire(timeout)
      if timeout.nil?
        @flush_lock.lock
        return true
      end

      deadline = Clock.monotonic + timeout
      until @flush_lock.try_lock
        return false if Clock.monotonic >= deadline

        sleep(0.005)
      end
      true
    end

    def send_all(taken, reports)
      start = 0
      while start < taken.size
        outcome = send_batch(taken[start, MAX_BATCH], reports)
        if outcome == :kept
          readd(taken[start, taken.size - start])
          return
        end
        start += MAX_BATCH
      end
    end

    def readd(entries)
      @lock.synchronize do
        entries.each { |key, count| @counts[key] = (@counts[key] || 0) + count }
      end
    end

    def send_batch(batch, reports)
      body = Json.generate({ "events" => batch.map do |(flag, index, day), count|
        { "flag" => flag, "variationIndex" => index, "count" => count, "day" => day }
      end })
      headers = @headers.merge("Content-Type" => "application/json")
      response = request(headers, body, reports)
      return :kept unless response

      status = response.status
      return :sent if status >= 200 && status < 300

      code = LastError.http(status)
      reports << [code, "usage request failed: #{code}"]
      hold(response) if status == 429
      self.class.retryable?(status) ? :kept : :dropped
    end

    def request(headers, body, reports)
      @transport.fetch(@endpoint, "POST", "/sdk/v1/events", headers, body, @request_timeout, nil)
    rescue Http::RequestTimeout
      reports << [LastError::TIMEOUT, "usage request failed: timeout"]
      nil
    rescue StandardError
      reports << [LastError::NETWORK, "usage request failed: network"]
      nil
    end

    def hold(response)
      seconds = self.class.retry_after(response)
      return unless seconds

      until_time = @clock.monotonic + seconds
      @lock.synchronize { @hold_until = [@hold_until, until_time].max }
    end
  end
end
