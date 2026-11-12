# frozen_string_literal: true

require_relative "support"
require "timeout"

# Ruby has no race detector, so these tests drive every operation from many threads at once and check
# that nothing raises, nothing deadlocks and every answer is one a single thread could have seen.
class ConcurrencyTest < Minitest::Test
  include Support
  include Seams

  CONTEXT = { "key" => "u" }.freeze

  def test_every_operation_from_many_threads
    server = build_server
    server.serve_ruleset
    server.route("POST", "/sdk/v1/events") { { status: 202 } }
    errors = Queue.new
    hits = Queue.new
    hook = ->(event) { hits << event.value }
    client = seamed_client(server, stream: false, poll_interval: 0.005, flush_interval: 0.005,
                                   hooks: Toggleslash::Hooks.new(after_evaluation: hook),
                                   on_error: ->(error) { errors << error })
    assert client.ready
    failures = Queue.new
    threads = Array.new(16) do |n|
      Thread.new do
        300.times do
          case n % 4
          when 0 then failures << "variation" unless client.variation("on", CONTEXT, false) == true
          when 1 then client.status
          when 2 then client.int_variation("num", CONTEXT, 0)
          else client.variation_detail("name", CONTEXT, "")
          end
        end
      rescue Exception => e # rubocop:disable Lint/RescueException
        failures << e
      end
    end
    Timeout.timeout(30) { threads.each(&:join) }
    assert_empty(failures.size.times.map { failures.pop })
    client.close
  end

  def test_close_racing_variation_and_a_second_close
    20.times do
      server = build_server
      server.serve_ruleset
      server.route("POST", "/sdk/v1/events") { { status: 202 } }
      client = seamed_client(server, stream: true, poll_interval: 0.005, flush_interval: 0.005)
      client.ready
      failures = Queue.new
      workers = Array.new(6) do
        Thread.new do
          200.times do
            client.variation("on", CONTEXT, false)
            client.status
          end
        rescue StandardError => e
          failures << e
        end
      end
      closers = Array.new(3) { Thread.new { client.close } }
      Timeout.timeout(20) { (workers + closers).each(&:join) }
      assert_empty(failures.size.times.map { failures.pop })
      assert_equal "closed", client.status.stream
      server.stop
    end
  end

  def test_close_while_the_ruleset_request_hangs_returns_within_twice_the_timeout
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { |_request, socket| sleep 0.01 until socket.closed? }
    client = seamed_client(server, request_timeout: 1.0)
    sleep 0.2
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    client.close
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 2.0
    assert_equal false, client.ready
  end

  def test_close_while_a_stream_is_open_returns_at_once
    server = build_server
    server.serve_ruleset
    server.route("GET", "/sdk/v1/stream") do |_request, socket|
      FakeServer.sse_head(socket)
      FakeServer.chunk(socket, "event: connected\ndata: \"v1\"\n\n")
      sleep 0.01 until socket.closed?
      nil
    end
    client = seamed_client(server, stream: true, request_timeout: 5)
    assert client.ready
    eventually { client.status.stream == "connected" }
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    client.close
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 1.0
    assert_equal "closed", client.status.stream
  end

  def test_ready_called_from_many_threads_agrees
    server = build_server
    server.serve_ruleset
    client = seamed_client(server)
    results = Array.new(8) { Thread.new { client.ready } }.map(&:value)
    assert_equal [true], results.uniq
  end
end
