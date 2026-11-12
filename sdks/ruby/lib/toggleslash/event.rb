# frozen_string_literal: true

module Toggleslash
  # A flag that is set once and waited on with a bound: a mutex and a condition variable, so a waiter
  # wakes at once when it is set and never polls.
  class Event
    def initialize
      @mutex = Mutex.new
      @cond = ConditionVariable.new
      @set = false
    end

    def set
      @mutex.synchronize do
        @set = true
        @cond.broadcast
      end
    end

    def set?
      @mutex.synchronize { @set }
    end

    # Waits until the event is set or `seconds` have passed. True when it is set.
    def wait(seconds)
      deadline = Clock.monotonic + seconds
      @mutex.synchronize do
        until @set
          remaining = deadline - Clock.monotonic
          return false if remaining <= 0

          @cond.wait(@mutex, remaining)
        end
        true
      end
    end
  end

  # Real time. A test passes an object with the same three methods.
  class Clock
    def self.monotonic
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    def monotonic
      Clock.monotonic
    end

    # The wall clock, as a UTC Time.
    def now
      Time.now.utc
    end

    # Waits for `event` up to `seconds`. True when it was set.
    def wait(event, seconds)
      event.wait(seconds.clamp(0.0, Client::MAX_WAIT))
    end
  end
end
