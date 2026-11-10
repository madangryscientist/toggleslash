# frozen_string_literal: true

require 'timeout'
require_relative 'test_support'

class StreamTest < Minitest::Test
  include ClientSupport

  def open_connected_stream(&after)
    server.on('/sdk/v1/stream') do |_request, io|
      FakeServer.open_stream(io)
      FakeServer.event(io, "event: connected\ndata: {}\n\n")
      after&.call(io)
    end
  end

  def test_a_change_event_with_a_new_tag_fetches_the_ruleset
    gate = Queue.new
    open_connected_stream do |io|
      gate.pop
      FakeServer.event(io, "event: changed\ndata: {}\n\n")
      sleep 3
    end
    client = build(stream: true)
    assert client.ready
    wait_until(5, 'connected') { client.status.stream == :connected }
    server.publish([server.flag('f', false, 2)])
    gate << true
    wait_until(5, 'the new ruleset') { client.status.etag == server.etag }
    assert_equal false, client.variation('f', CONTEXT, true)
  end

  def test_a_stream_that_goes_quiet_is_dropped_as_a_timeout_through_the_idle_seam
    open_connected_stream { |_io| sleep 5 }
    client = build(stream: true, stream_idle_timeout: 0.4, poll_interval: 5)
    assert client.ready
    wait_until(5, 'connected') { client.status.stream == :connected }
    wait_until(5, 'the watchdog') { client.status.stream == :disconnected }
    assert_equal :timeout, client.status.last_error
  end

  def test_a_keep_alive_restarts_the_idle_watchdog
    open_connected_stream do |io|
      12.times do
        sleep 0.15
        FakeServer.event(io, ": keep-alive\n\n")
      end
      sleep 5
    end
    client = build(stream: true, stream_idle_timeout: 0.4, poll_interval: 5)
    assert client.ready
    wait_until(5, 'connected') { client.status.stream == :connected }
    sleep 1.2
    assert_equal :connected, client.status.stream
    assert_nil client.status.last_error
  end

  def test_a_stream_answered_with_an_error_status_is_disconnected
    server.on('/sdk/v1/stream') { |_request, io| FakeServer.respond(io, 503) }
    client = build(stream: true, poll_interval: 5)
    assert client.ready
    wait_until(5, 'the failure') { client.status.last_error == :http_503 }
    assert_equal :disconnected, client.status.stream
  end

  def test_close_abandons_a_stream_that_is_open_and_reports_nothing
    open_connected_stream { |_io| sleep 10 }
    errors = []
    client = build(stream: true, on_error: ->(error) { errors << error })
    assert client.ready
    wait_until(5, 'connected') { client.status.stream == :connected }
    started = ToggleSlash::Clock.monotonic
    client.close
    assert_operator ToggleSlash::Clock.monotonic - started, :<, 2
    assert_empty errors
    assert_equal :closed, client.status.stream
  end
end
