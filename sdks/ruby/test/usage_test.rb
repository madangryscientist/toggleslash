# frozen_string_literal: true

require_relative "support"
require "timeout"

class UsageTest < Minitest::Test
  include Support
  include Seams

  CONTEXT = { "key" => "SECRET-CTX-KEY", "attributes" => { "plan" => "SECRET-ATTR" } }.freeze

  def events_route(server, status: 202, headers: {})
    server.route("POST", "/sdk/v1/events") { { status: status, headers: headers } }
  end

  def sent(server)
    server.requests_to("/sdk/v1/events").flat_map { |request| Toggleslash::Json.parse(request.body)["events"] }
  end

  def client_with(server, **)
    server.serve_ruleset
    client = seamed_client(server, **)
    assert client.ready
    client
  end

  def test_counts_are_summed_per_flag_variation_and_day_and_sent_on_close
    server = build_server
    events_route(server)
    clock = TestClock.new
    client = client_with(server, clock: clock)
    clock.today = Time.utc(2026, 11, 12, 23, 59, 59)
    3.times { client.variation("on", CONTEXT, false) }
    clock.today = Time.utc(2026, 11, 13, 0, 0, 1)
    2.times { client.variation("on", CONTEXT, false) }
    client.variation("name", CONTEXT, "")
    client.close
    request = server.requests_to("/sdk/v1/events").first
    assert_equal "Bearer sdk-secret-key", request.headers["authorization"]
    assert_equal "application/json", request.headers["content-type"]
    counts = sent(server).sort_by { |entry| [entry["flag"], entry["day"]] }
    assert_equal([["name", 1, "2026-11-13", 1], ["on", 1, "2026-11-12", 3], ["on", 1, "2026-11-13", 2]],
                 counts.map { |entry| [entry["flag"], entry["variationIndex"], entry["day"], entry["count"]] })
  end

  def test_a_fallback_a_mismatch_an_unknown_flag_and_an_invalid_context_count_nothing
    server = build_server
    events_route(server)
    client = client_with(server)
    client.variation("nope", CONTEXT, false)
    client.variation("on", CONTEXT, "text")
    client.variation("on", "not a context", false)
    client.close
    assert_empty server.requests_to("/sdk/v1/events")
  end

  def test_nothing_in_a_request_or_an_error_carries_the_context_or_the_key_beyond_authorization
    server = build_server
    events_route(server)
    errors = []
    client = client_with(server, on_error: ->(error) { errors << error.message }, poll_interval: 0.02)
    client.variation("on", CONTEXT, false)
    client.variation("on", { "key" => 5 }, false)
    client.int_variation("frac", CONTEXT, 0)
    client.close
    server.requests.each do |request|
      raw = request.headers.except("authorization").to_a.flatten.join(" ") + request.body + request.path
      refute_includes raw, "SECRET"
      refute_includes raw, "sdk-secret-key"
    end
    errors.each { |message| refute_match(/SECRET|sdk-secret-key/, message) }
    status = client.status
    refute_match(/SECRET|sdk-secret-key/, status.to_h.to_s)
  end

  def test_only_the_allowed_headers_are_sent
    server = build_server
    events_route(server)
    server.route("GET", "/sdk/v1/stream") do |_request, socket|
      FakeServer.sse_head(socket)
      FakeServer.chunk(socket, "event: connected\ndata: \"v1\"\n\n")
      sleep 0.01 until socket.closed?
      nil
    end
    client = client_with(server, stream: true, poll_interval: 0.02)
    client.variation("on", CONTEXT, false)
    client.close
    allowed = %w[host connection content-length authorization accept if-none-match content-type user-agent]
    server.requests.each { |request| assert_empty request.headers.keys - allowed, request.path }
  end

  def test_more_than_500_entries_go_in_requests_of_at_most_500
    server = build_server
    events_route(server)
    flags = Array.new(1201) { |i| FakeServer.flag("f#{i}", [false, true], 1) }
    server.serve_ruleset(FakeServer.ruleset(*flags))
    client = seamed_client(server)
    assert client.ready
    1201.times { |i| client.variation("f#{i}", CONTEXT, false) }
    client.close
    sizes = server.requests_to("/sdk/v1/events").map { |request| Toggleslash::Json.parse(request.body)["events"].size }
    assert_equal [1201], [sizes.sum]
    assert_equal 3, sizes.size
    assert(sizes.all? { |size| size <= 500 })
  end

  def test_counts_sent_every_flush_interval_and_nothing_when_nothing_is_pending
    server = build_server
    events_route(server)
    client = client_with(server, flush_interval: 0.05)
    sleep 0.2
    assert_empty server.requests_to("/sdk/v1/events")
    client.variation("on", CONTEXT, false)
    eventually { server.requests_to("/sdk/v1/events").size == 1 }
    sleep 0.2
    assert_equal 1, server.requests_to("/sdk/v1/events").size
  end

  def test_5xx_408_and_429_keep_the_counts_and_other_4xx_drop_them
    [500, 408, 429].each do |status|
      server = build_server
      answer = status
      server.route("POST", "/sdk/v1/events") { { status: answer } }
      client = client_with(server, flush_interval: 0.03)
      client.variation("on", CONTEXT, false)
      eventually { server.requests_to("/sdk/v1/events").size >= 2 }
      answer = 202
      eventually { sent(server).size >= 3 }
      client.close
      assert_equal [1], sent(server).map { |entry| entry["count"] }.uniq, status.to_s
    end
    server = build_server
    server.route("POST", "/sdk/v1/events") { { status: 400 } }
    errors = Queue.new
    client = client_with(server, flush_interval: 0.03, on_error: ->(error) { errors << error })
    client.variation("on", CONTEXT, false)
    eventually { server.requests_to("/sdk/v1/events").size == 1 }
    sleep 0.2
    assert_equal 1, server.requests_to("/sdk/v1/events").size
    assert_equal "http_400", errors.pop.code
  end

  def test_a_retry_after_above_300_holds_usage_for_300_and_close_reports_unsent_counts
    server = build_server
    answers = [{ status: 429, headers: { "Retry-After" => "99999" } }]
    server.route("POST", "/sdk/v1/events") { answers.first || { status: 202 } }
    clock = TestClock.new
    errors = Queue.new
    client = client_with(server, clock: clock, flush_interval: 0.02, on_error: ->(error) { errors << error })
    client.variation("on", CONTEXT, false)
    eventually { server.requests_to("/sdk/v1/events").size == 1 }
    sleep 0.2
    assert_equal 1, server.requests_to("/sdk/v1/events").size
    clock.offset = 299.0
    sleep 0.15
    assert_equal 1, server.requests_to("/sdk/v1/events").size
    answers.clear
    clock.offset = 301.0
    eventually { server.requests_to("/sdk/v1/events").size >= 2 }
  end

  def test_close_during_a_usage_hold_does_not_wait_and_tells_on_error
    server = build_server
    server.route("POST", "/sdk/v1/events") { { status: 429, headers: { "Retry-After" => "120" } } }
    errors = Queue.new
    client = client_with(server, flush_interval: 0.03, on_error: ->(error) { errors << error })
    client.variation("on", CONTEXT, false)
    eventually { server.requests_to("/sdk/v1/events").size == 1 }
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    client.close
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 2
    messages = []
    messages << errors.pop until errors.empty?
    assert_includes messages.map(&:message), "closed with 1 usage counts unsent: the server asked to wait"
    assert_equal 1, server.requests_to("/sdk/v1/events").size
  end

  def test_a_usage_request_that_hangs_is_bounded_and_variation_stays_fast
    server = build_server
    server.route("POST", "/sdk/v1/events") { |_request, socket| sleep 0.01 until socket.closed? }
    errors = Queue.new
    client = client_with(server, flush_interval: 0.03, request_timeout: 0.4, on_error: ->(error) { errors << error })
    client.variation("on", CONTEXT, false)
    eventually { server.requests_to("/sdk/v1/events").size >= 1 }
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    300.times { client.variation("on", CONTEXT, false) }
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 0.3
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    client.close
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, (4 * 0.4) + 0.5
    assert_equal "timeout", errors.pop.code
  end
end
