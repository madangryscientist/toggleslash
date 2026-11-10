# frozen_string_literal: true

module ToggleSlash
  # The real clocks. A client takes any object with these two methods as its `clock:` option, which is how
  # the tests move time without waiting for it.
  module Clock
    module_function

    # Wall time: usage days and `last_sync_at`.
    def now
      Time.now
    end

    # Monotonic seconds: the Retry-After hold, which a wall clock change must not move. Request deadlines
    # and waits always use this real clock, so a fake `monotonic` moves a hold and nothing else.
    def monotonic
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end
  end
end
