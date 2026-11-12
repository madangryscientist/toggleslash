# frozen_string_literal: true

require_relative "test_helper"
require_relative "fake_server"

# Helpers shared by the client tests.
module Support
  # Polls `block` until it is truthy or `seconds` pass. A test waits on a condition, never for a fixed time.
  def eventually(seconds = 10.0, what = "the condition")
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + seconds
    loop do
      value = yield
      return value if value

      flunk "timed out waiting for #{what}" if Process.clock_gettime(Process::CLOCK_MONOTONIC) > deadline

      sleep 0.005
    end
  end

  def build_server(**)
    server = FakeServer.new(**)
    (@servers ||= []) << server
    server
  end

  def build_client(server, **options)
    options = { stream: false, poll_interval: 60, flush_interval: 60, request_timeout: 5 }.merge(options)
    client = Toggleslash.create(url: server.url, sdk_key: "sdk-secret-key", **options)
    (@clients ||= []) << client
    client
  end

  def teardown
    (@clients || []).each(&:close)
    (@servers || []).each(&:stop)
    super
  end
end

# A clock the tests steer: `offset` moves monotonic time (a hold of 300 s passes without waiting), `today`
# pins the wall clock, and every wait is recorded but lasts at most 20 ms of real time.
class TestClock < Toggleslash::Clock
  attr_accessor :offset, :today
  attr_reader :waits

  def initialize
    super
    @offset = 0.0
    @today = nil
    @waits = []
    @lock = Mutex.new
  end

  def monotonic
    super + @offset
  end

  def now
    @today || (Time.now.utc + @offset)
  end

  def wait(event, seconds)
    @lock.synchronize { @waits << seconds }
    event.wait([seconds, 0.02].min)
  end
end

# Builds a client with the private seams the SDK keeps for tests.
module Seams
  def seamed_client(server, **options)
    options = { stream: false, poll_interval: 60, flush_interval: 60, request_timeout: 5 }.merge(options)
    client = Toggleslash::Client.new(url: server.url, sdk_key: "sdk-secret-key", **options).start
    (@clients ||= []) << client
    client
  end
end
