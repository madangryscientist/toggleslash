# frozen_string_literal: true

require_relative "support"
require "timeout"

class SyncTest < Minitest::Test
  include Support
  include Seams

  CONTEXT = { "key" => "u" }.freeze

  def stream_route(server, events, hold: true)
    server.route("GET", "/sdk/v1/stream") do |_request, socket|
      FakeServer.sse_head(socket)
      events.each { |event| FakeServer.chunk(socket, event) }
      sleep 0.01 while hold && !socket.closed?
      nil
    end
  end

  def test_the_ruleset_request_headers_and_the_conditional_request
    server = build_server
    server.serve_ruleset
    client = build_client(server, poll_interval: 0.02)
    assert client.ready
    eventually { server.requests_to("/sdk/v1/ruleset").size >= 3 }
    first, second = server.requests_to("/sdk/v1/ruleset")
    assert_nil first.headers["if-none-match"]
    assert_equal '"v1"', second.headers["if-none-match"]
    assert_equal "Bearer sdk-secret-key", first.headers["authorization"]
    assert_equal "application/json", first.headers["accept"]
    assert_equal '"v1"', client.status.etag
  end

  def test_an_etag_is_kept_exactly_as_received
    server = build_server
    server.serve_ruleset(FakeServer::RULESET, etag: 'W/"weird tag"')
    client = build_client(server, poll_interval: 0.02)
    assert client.ready
    assert_equal 'W/"weird tag"', client.status.etag
    eventually { server.requests_to("/sdk/v1/ruleset").size >= 2 }
    assert_equal 'W/"weird tag"', server.requests_to("/sdk/v1/ruleset")[1].headers["if-none-match"]
  end

  def test_a_missing_or_empty_etag_is_none_and_nothing_conditional_is_sent
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { { status: 200, body: FakeServer::RULESET, headers: { "ETag" => "" } } }
    client = build_client(server, poll_interval: 0.02)
    assert client.ready
    assert_nil client.status.etag
    eventually { server.requests_to("/sdk/v1/ruleset").size >= 2 }
    assert(server.requests_to("/sdk/v1/ruleset").none? { |request| request.headers.key?("if-none-match") })
  end

  def test_a_304_with_no_ruleset_held_is_http_304
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { { status: 304 } }
    client = build_client(server)
    refute client.ready
    assert_equal "http_304", client.status.last_error
    assert_nil client.status.last_sync_at
  end

  def test_error_statuses_network_timeout_and_bad_bodies_set_last_error
    {
      401 => "http_401", 500 => "http_500", 404 => "http_404"
    }.each do |status, code|
      server = build_server
      server.route("GET", "/sdk/v1/ruleset") { { status: status } }
      client = build_client(server)
      refute client.ready
      assert_equal code, client.status.last_error
    end
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { { body: "not json" } }
    client = build_client(server)
    refute client.ready
    assert_equal "bad_ruleset", client.status.last_error
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { { body: '{"flags":{},"segments":[]}' } }
    refute build_client(server).ready
  end

  def test_a_bad_ruleset_keeps_the_one_held
    server = build_server
    answers = [FakeServer::RULESET, "garbage"]
    server.route("GET", "/sdk/v1/ruleset") { { body: answers.first, headers: { "ETag" => '"x"' } } }
    client = build_client(server, poll_interval: 0.02)
    assert client.ready
    answers[0] = answers[1]
    eventually { client.status.last_error == "bad_ruleset" }
    assert_equal true, client.variation("on", CONTEXT, false)
    assert_equal '"x"', client.status.etag
  end

  def test_a_refused_connection_is_a_network_error
    server = build_server
    url = server.url
    server.stop
    client = Toggleslash.create(url: url, sdk_key: "k", stream: false, request_timeout: 2)
    (@clients ||= []) << client
    refute client.ready
    assert_equal "network", client.status.last_error
  end

  def test_a_request_that_never_answers_is_a_timeout_and_variation_stays_fast
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { |_request, socket| sleep 0.01 until socket.closed? }
    client = build_client(server, request_timeout: 0.3)
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    200.times { client.variation("on", CONTEXT, false) }
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 0.25
    refute client.ready
    assert_equal "timeout", client.status.last_error
  end

  def test_a_body_that_never_finishes_is_a_timeout
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") do |_request, socket|
      FakeServer.head(socket, 200, "Content-Length" => 1000)
      socket.write("{")
      sleep 0.01 until socket.closed?
      nil
    end
    client = build_client(server, request_timeout: 0.3)
    refute client.ready
    assert_equal "timeout", client.status.last_error
  end

  def test_a_429_retry_after_above_300_is_held_for_300_on_the_ruleset
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { { status: 429, headers: { "Retry-After" => "99999" } } }
    clock = TestClock.new
    client = seamed_client(server, clock: clock, poll_interval: 0.01)
    refute client.ready
    first = server.requests_to("/sdk/v1/ruleset").size
    sleep 0.2
    assert_equal first, server.requests_to("/sdk/v1/ruleset").size
    assert_operator clock.waits.max, :<=, 300.0
    assert_operator clock.waits.max, :>, 299.0
    clock.offset = 299.0
    sleep 0.2
    assert_equal first, server.requests_to("/sdk/v1/ruleset").size
    clock.offset = 301.0
    eventually { server.requests_to("/sdk/v1/ruleset").size > first }
  end

  def test_a_retry_after_that_is_not_whole_seconds_is_ignored
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { { status: 429, headers: { "Retry-After" => "1.5" } } }
    clock = TestClock.new
    client = seamed_client(server, clock: clock, poll_interval: 0.01)
    refute client.ready
    eventually { server.requests_to("/sdk/v1/ruleset").size >= 3 }
    assert_equal "http_429", client.status.last_error
  end

  def test_a_429_on_the_stream_holds_the_ruleset_too
    server = build_server
    server.serve_ruleset
    server.route("GET", "/sdk/v1/stream") { { status: 429, headers: { "Retry-After" => "99999" } } }
    clock = TestClock.new
    client = seamed_client(server, clock: clock, stream: true, poll_interval: 0.01)
    assert client.ready
    eventually { server.requests_to("/sdk/v1/stream").size == 1 }
    sleep 0.2
    assert_equal 1, server.requests_to("/sdk/v1/stream").size
    assert_equal 1, server.requests_to("/sdk/v1/ruleset").size
    clock.offset = 301.0
    eventually { server.requests_to("/sdk/v1/stream").size >= 2 }
  end

  def test_a_stream_event_naming_the_held_tag_fetches_nothing_and_a_new_tag_fetches
    server = build_server
    server.serve_ruleset
    stream_route(server, ["event: connected\ndata: \"v1\"\n\n", ": ka\n\n", "event: changed\ndata: \"v1\"\n\n"])
    client = build_client(server, stream: true)
    assert client.ready
    eventually { client.status.stream == "connected" }
    sleep 0.1
    assert_equal 1, server.requests_to("/sdk/v1/ruleset").size
    assert_equal "Bearer sdk-secret-key", server.requests_to("/sdk/v1/stream").first.headers["authorization"]
    assert_equal "text/event-stream", server.requests_to("/sdk/v1/stream").first.headers["accept"]
  end

  def test_a_changed_event_with_a_new_tag_fetches_the_ruleset
    server = build_server
    tag = '"a"'
    server.route("GET", "/sdk/v1/ruleset") do |_request, _socket|
      { body: FakeServer.ruleset(FakeServer.flag("f", [false, true], tag == '"a"' ? 0 : 1)),
        headers: { "ETag" => tag } }
    end
    server.route("GET", "/sdk/v1/stream") do |_request, socket|
      FakeServer.sse_head(socket)
      FakeServer.chunk(socket, "event: connected\ndata: \"a\"\n\n")
      sleep 0.01 until tag != '"a"'
      FakeServer.chunk(socket, "event: changed\ndata: \"b\"\n\n")
      sleep 0.01 until socket.closed?
      nil
    end
    client = build_client(server, stream: true)
    assert client.ready
    assert_equal false, client.variation("f", CONTEXT, true)
    tag = '"b"'
    eventually { client.variation("f", CONTEXT, false) == true }
    assert_equal '"b"', client.status.etag
  end

  def test_a_stream_that_goes_quiet_for_the_watchdog_ends_and_reconnects
    server = build_server
    server.serve_ruleset
    stream_route(server, ["event: connected\ndata: \"v1\"\n\n"])
    messages = Queue.new
    client = seamed_client(server, stream: true, stream_watchdog: 0.3, poll_interval: 0.05,
                                   on_error: ->(error) { messages << error.message })
    assert client.ready
    eventually(10, "a second stream request") { server.requests_to("/sdk/v1/stream").size >= 2 }
    assert_equal "stream failed: timeout", messages.pop
  end

  def test_a_keep_alive_restarts_the_watchdog
    server = build_server
    server.serve_ruleset
    server.route("GET", "/sdk/v1/stream") do |_request, socket|
      FakeServer.sse_head(socket)
      FakeServer.chunk(socket, "event: connected\ndata: \"v1\"\n\n")
      20.times do
        sleep 0.1
        FakeServer.chunk(socket, ": ka\n\n")
      end
      nil
    end
    client = seamed_client(server, stream: true, stream_watchdog: 0.5, poll_interval: 0.05)
    assert client.ready
    sleep 1.8
    assert_equal 1, server.requests_to("/sdk/v1/stream").size
    assert_equal "connected", client.status.stream
  end

  def test_status_never_shows_a_stream_failure_beside_connected
    server = build_server
    server.serve_ruleset
    server.route("GET", "/sdk/v1/stream") do |_request, socket|
      FakeServer.sse_head(socket)
      FakeServer.chunk(socket, "event: connected\ndata: \"v1\"\n\n")
      sleep 0.005
      socket.close
      nil
    end
    client = seamed_client(server, stream: true, poll_interval: 0.01)
    assert client.ready
    bad = 0
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 1.5
    reader = Thread.new do
      while Process.clock_gettime(Process::CLOCK_MONOTONIC) < deadline
        status = client.status
        bad += 1 if status.stream == "connected" && status.last_error
        sleep 0.0002
      end
    end
    reader.join
    assert_equal 0, bad
    assert_operator server.requests_to("/sdk/v1/stream").size, :>, 2
  end

  def test_a_stream_answering_200_with_content_length_zero_is_http_200
    server = build_server
    server.serve_ruleset
    server.route("GET", "/sdk/v1/stream") { { status: 200, body: "" } }
    client = build_client(server, stream: true)
    assert client.ready
    eventually { client.status.last_error == "http_200" }
    assert_equal "disconnected", client.status.stream
  end

  def test_a_stream_that_ends_cleanly_with_no_bytes_leaves_last_error_alone
    server = build_server
    server.serve_ruleset
    server.route("GET", "/sdk/v1/stream") do |_request, socket|
      FakeServer.head(socket, 200, "Content-Type" => "text/event-stream")
      nil
    end
    client = build_client(server, stream: true)
    assert client.ready
    eventually { server.requests_to("/sdk/v1/stream").size >= 1 && client.status.stream == "disconnected" }
    assert_nil client.status.last_error
  end

  def test_a_ruleset_swap_is_seen_whole_by_a_reader
    server = build_server
    version = 1
    server.route("GET", "/sdk/v1/ruleset") do |_request, _socket|
      current = version
      { body: FakeServer.ruleset(FakeServer.flag("on", [false, true], 1, version: current)),
        headers: { "ETag" => "\"#{current}\"" } }
    end
    events = Queue.new
    hook = ->(event) { events << event }
    client = build_client(server, poll_interval: 0.003, hooks: Toggleslash::Hooks.new(after_evaluation: hook))
    assert client.ready
    flipper = Thread.new do
      200.times do
        version += 1
        sleep 0.002
      end
    end
    readers = Array.new(4) do
      Thread.new { 600.times { client.variation("on", CONTEXT, false) } }
    end
    (readers + [flipper]).each(&:join)
    mismatched = 0
    seen = 0
    events.size.times do
      event = events.pop
      seen += 1
      mismatched += 1 unless event.etag == "\"#{event.flag_version}\""
    end
    assert_operator seen, :>, 1000
    assert_equal 0, mismatched
  end

  def test_a_client_never_closed_does_not_keep_the_process_alive
    server = build_server
    server.serve_ruleset
    script = "$LOAD_PATH.unshift(#{File.expand_path("../lib", __dir__).inspect}); require 'toggleslash'; " \
             "c = Toggleslash.create(url: #{server.url.inspect}, sdk_key: 'k'); c.ready; puts :done"
    ruby = [RbConfig.ruby]
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    output = IO.popen([*ruby, "-e", script], err: File::NULL, &:read)
    assert_equal "done\n", output
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 8
  end
end
