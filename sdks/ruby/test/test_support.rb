# frozen_string_literal: true

require_relative 'test_helper'
require_relative 'fake_server'

# Helpers the client tests share.
module ClientSupport
  CONTEXT = { 'key' => 'u1' }.freeze

  # A clock whose wall time a test sets and whose monotonic time is the real one.
  class FakeClock
    attr_accessor :time

    def initialize(time)
      @time = time
    end

    def now
      @time
    end

    def monotonic
      ToggleSlash::Clock.monotonic
    end
  end

  def server
    @server ||= FakeServer.new
  end

  def clients
    @clients ||= []
  end

  def build(**options)
    options = { url: server.url, sdk_key: 'k', stream: false, request_timeout: 2 }.merge(options)
    client = ToggleSlash.create(**options)
    clients << client
    client
  end

  def teardown
    clients.each { |client| Timeout.timeout(10) { client.close } }
    @server&.stop
    super
  end

  # Polls the block until it is true; fails after `seconds`.
  def wait_until(seconds = 5, what = 'the condition')
    deadline = ToggleSlash::Clock.monotonic + seconds
    until yield
      flunk "timed out waiting for #{what}" if ToggleSlash::Clock.monotonic > deadline
      sleep 0.01
    end
  end
end
