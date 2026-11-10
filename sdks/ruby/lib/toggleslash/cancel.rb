# frozen_string_literal: true

require_relative 'clock'

module ToggleSlash
  # A one-way switch that can be waited on two ways: with a timeout (a sleep that ends the moment the switch
  # is thrown) and through a pipe a thread can hand to `IO.select` next to its socket, so a request blocked
  # on the network is abandoned at once.
  class Cancel
    def initialize
      @mutex = Mutex.new
      @condition = ConditionVariable.new
      @cancelled = false
      @reader = nil
      @writer = nil
    end

    def cancelled?
      @mutex.synchronize { @cancelled }
    end

    def cancel
      @mutex.synchronize do
        return if @cancelled

        @cancelled = true
        @condition.broadcast
        wake_pipe
      end
    end

    # Sleeps up to `seconds`; true when the switch was thrown before or during the wait.
    def wait(seconds)
      deadline = Clock.monotonic + seconds
      @mutex.synchronize do
        until @cancelled
          remaining = deadline - Clock.monotonic
          break unless remaining.positive?

          @condition.wait(@mutex, remaining)
        end
        @cancelled
      end
    end

    # The read end of the pipe: readable once the switch is thrown. Made on first use.
    def reader
      @mutex.synchronize do
        if @reader.nil?
          @reader, @writer = IO.pipe
          wake_pipe if @cancelled
        end
        @reader
      end
    end

    # Closes the pipe. Called once, after every thread that used it has been joined.
    def release
      @mutex.synchronize do
        [@reader, @writer].compact.each { |io| io.close unless io.closed? }
      end
    end

    private

    def wake_pipe
      @writer&.write_nonblock('x', exception: false)
    rescue IOError, SystemCallError
      nil
    end
  end
end
