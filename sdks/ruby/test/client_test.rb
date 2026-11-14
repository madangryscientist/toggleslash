# frozen_string_literal: true

require_relative "support"
require "timeout"

class ClientTest < Minitest::Test
  include Support

  CONTEXT = { "key" => "u" }.freeze

  def seamed_client(server, **)
    client = Toggleslash::Client.new(url: server.url, sdk_key: "k", stream: false, poll_interval: 60,
                                     flush_interval: 60,
                                     request_timeout: 5, **).start
    (@clients ||= []) << client
    client
  end

  def ready_client(**)
    server = build_server
    server.serve_ruleset
    client = build_client(server, **)
    assert client.ready
    [server, client]
  end

  def test_evaluates_after_the_first_load
    _server, client = ready_client
    detail = client.variation_detail("on", { "key" => "u" }, false)
    assert_equal true, detail.value
    assert_equal 1, detail.variation_index
    assert_equal "FALLTHROUGH", detail.reason.kind
  end

  def test_not_ready_before_a_load_answers_the_fallback
    server = build_server
    gate = Queue.new
    server.route("GET", "/sdk/v1/ruleset") { |_request, _socket| gate.pop && { status: 500 } }
    client = build_client(server)
    detail = client.variation_detail("on", { "key" => "u" }, "fb")
    assert_equal "fb", detail.value
    assert_nil detail.variation_index
    assert_equal %w[ERROR NOT_READY], [detail.reason.kind, detail.reason.code]
    gate << true
  end

  def test_unknown_flag_gives_the_fallback_and_the_evaluator_reason
    _server, client = ready_client
    detail = client.variation_detail("nope", { "key" => "u" }, 7)
    assert_equal 7, detail.value
    assert_equal "FLAG_NOT_FOUND", detail.reason.code
  end

  def test_type_mismatch_gives_the_fallback
    _server, client = ready_client
    detail = client.variation_detail("on", { "key" => "u" }, "text")
    assert_equal "text", detail.value
    assert_nil detail.variation_index
    assert_equal "TYPE_MISMATCH", detail.reason.code
  end

  def test_a_null_fallback_accepts_any_kind
    _server, client = ready_client
    assert_equal true, client.variation("on", { "key" => "u" }, nil)
    assert_equal "b", client.variation("name", { "key" => "u" }, nil)
  end

  def test_a_class_with_no_json_kind_never_matches
    _server, client = ready_client
    fallback = Object.new
    detail = client.variation_detail("on", { "key" => "u" }, fallback)
    assert_same fallback, detail.value
    assert_equal "TYPE_MISMATCH", detail.reason.code
    assert_equal "TYPE_MISMATCH", client.variation_detail("on", { "key" => "u" }, :sym).reason.code
  end

  # `variation` is dynamically typed: an integer fallback is the JSON kind number, so a served 2.0 comes back
  # as served. The integer accessor is the typed path (the next test); this is a recorded n/a in RUN.md.
  def test_the_generic_accessor_with_an_integer_fallback_returns_the_served_number
    server = build_server
    server.serve_ruleset(FakeServer.ruleset(FakeServer.flag("two", [2.0, 2.5, 3], 0)))
    client = build_client(server)
    assert client.ready
    assert_equal 2.0, client.variation("two", CONTEXT, 0)
    assert_equal 2, client.int_variation("two", CONTEXT, 0)
  end

  def test_the_integer_accessor
    server = build_server
    server.serve_ruleset(FakeServer.ruleset(
                           FakeServer.flag("two", [2.0], 0), FakeServer.flag("frac", [2.5], 0),
                           FakeServer.flag("max", [9_223_372_036_854_774_784], 0),
                           FakeServer.flag("min", [-9_223_372_036_854_775_808], 0),
                           FakeServer.flag("over", [9_223_372_036_854_775_808], 0),
                           FakeServer.flag("under", [-9_223_372_036_854_777_856], 0),
                           FakeServer.flag("float_over", [1.0e30], 0)
                         ))
    client = build_client(server)
    assert client.ready
    value = client.int_variation("two", CONTEXT, 0)
    assert_instance_of Integer, value
    assert_equal 2, value
    assert_equal 9_223_372_036_854_774_784, client.int_variation("max", CONTEXT, 0)
    assert_equal(-9_223_372_036_854_775_808, client.int_variation("min", CONTEXT, 0))
    %w[frac over under float_over].each do |flag|
      detail = client.int_variation_detail(flag, CONTEXT, 5)
      assert_equal 5, detail.value, flag
      assert_equal "TYPE_MISMATCH", detail.reason.code, flag
    end
    assert_equal "TYPE_MISMATCH", client.int_variation_detail("two", CONTEXT, "x").reason.code
  end

  # A nil fallback says nothing about the kind, but the integer accessor's own kind is a number: a served
  # string is TYPE_MISMATCH (contract 4, "The mismatch rule").
  def test_the_integer_accessor_with_a_nil_fallback_accepts_a_number_only
    server = build_server
    server.serve_ruleset(FakeServer.ruleset(FakeServer.flag("two", [2.0], 0), FakeServer.flag("text", ["a"], 0),
                                            FakeServer.flag("frac", [2.5], 0)))
    client = build_client(server)
    assert client.ready
    assert_equal 2, client.int_variation("two", CONTEXT, nil)
    text = client.int_variation_detail("text", CONTEXT, nil)
    assert_nil text.value
    assert_equal "TYPE_MISMATCH", text.reason.code
    assert_equal "TYPE_MISMATCH", client.int_variation_detail("frac", CONTEXT, nil).reason.code
  end

  def test_a_hook_sees_the_value_for_the_integer_accessor_as_an_integer
    events = []
    server = build_server
    server.serve_ruleset(FakeServer.ruleset(FakeServer.flag("two", [2.0], 0, version: 4)))
    client = build_client(server, hooks: Toggleslash::Hooks.new(after_evaluation: ->(event) { events << event }))
    assert client.ready
    client.int_variation("two", { "key" => "u" }, 0)
    assert_equal 1, events.size
    assert_instance_of Integer, events[0].value
    assert_equal 4, events[0].flag_version
    assert_equal '"v1"', events[0].etag
  end

  def test_a_hook_that_raises_goes_to_on_error_and_the_answer_stands
    errors = Queue.new
    server = build_server
    server.serve_ruleset
    hook = ->(_event) { raise "hook broke" }
    client = build_client(server, hooks: Toggleslash::Hooks.new(after_evaluation: hook), on_error: lambda { |e|
      errors << e
    })
    assert client.ready
    assert_equal true, client.variation("on", { "key" => "u" }, false)
    assert_equal "hook broke", errors.pop.message
  end

  def test_on_error_that_raises_is_swallowed
    server = build_server
    server.route("GET", "/sdk/v1/ruleset") { { status: 500 } }
    client = build_client(server, on_error: ->(_e) { raise "nope" })
    refute client.ready
    assert_equal "http_500", client.status.last_error
  end

  def test_an_evaluator_that_raises_gives_the_fallback_and_one_on_error
    errors = Queue.new
    server = build_server
    server.serve_ruleset
    client = seamed_client(server, on_error: ->(e) { errors << e }, evaluator: ->(*) { raise "boom" })
    assert client.ready
    detail = client.variation_detail("on", { "key" => "u" }, "fb")
    assert_equal "fb", detail.value
    assert_equal %w[ERROR EVALUATION_FAILED], [detail.reason.kind, detail.reason.code]
    error = errors.pop
    assert_equal "EVALUATION_FAILED", error.code
    refute_includes error.message, "boom"
    assert errors.empty?
  end

  def test_an_evaluator_that_raises_a_deep_stack_error_is_contained
    server = build_server
    server.serve_ruleset
    client = seamed_client(server, evaluator: ->(*) { raise SystemStackError })
    assert client.ready
    assert_equal "EVALUATION_FAILED", client.variation_detail("on", { "key" => "u" }, 1).reason.code
  end

  def test_callbacks_that_call_back_into_the_client_do_not_deadlock
    server = build_server
    server.serve_ruleset
    inner = Queue.new
    client_box = []
    inside = false
    hook = lambda do |_event|
      next if inside

      inside = true
      client_box[0].variation("on", { "key" => "x" }, false)
      client_box[0].status
      inner << client_box[0].ready
    end
    client = build_client(server, hooks: Toggleslash::Hooks.new(after_evaluation: hook))
    client_box << client
    assert client.ready
    client.variation("on", { "key" => "u" }, false)
    assert_equal true, Timeout.timeout(5) { inner.pop }
  end

  def test_ready_and_close_from_the_sdks_own_thread_return_at_once
    server = build_server
    server.serve_ruleset
    release = Queue.new
    results = Queue.new
    client_box = []
    # Every error closes, as an application might: the final flush's failure reaches on_error on the thread
    # running shutdown, and that close must not wait for the shutdown it is inside.
    on_error = lambda do |_error|
      ready = client_box[0].ready
      started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
      closed = client_box[0].close
      results << [ready, closed, Process.clock_gettime(Process::CLOCK_MONOTONIC) - started]
    end
    client = build_client(server, poll_interval: 0.05, flush_interval: 3600, request_timeout: 5, on_error: on_error)
    client_box << client
    assert client.ready
    client.variation("on", { "key" => "u" }, false) # a count is pending, so the final flush goes out
    server.route("POST", "/sdk/v1/events") do |_request, _socket|
      release.pop
      { status: 202 }
    end
    server.route("GET", "/sdk/v1/ruleset") { { status: 500 } }
    ready, closed, took = Timeout.timeout(5) { results.pop }
    assert_equal [true, nil], [ready, closed]
    # Contract 6.4: close returns without waiting for the final flush, which hangs for request_timeout.
    assert_operator took, :<, 1.0
    eventually(5, "the client to read closed") { client.status.stream == Toggleslash::StreamState::CLOSED }
    eventually(5, "the final flush") { server.requests_to("/sdk/v1/events").any? }
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    client.close # waits for the shutdown: the final flush gives up after request_timeout
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 8.0
    _ready, _closed, later = Timeout.timeout(1) { results.pop }
    assert_operator later, :<, 1.0
  ensure
    release&.push(true)
  end

  def test_inspect_hides_the_key
    _server, client = ready_client
    refute_includes client.inspect, "sdk-secret-key"
    refute_includes client.to_s, "sdk-secret-key"
  end

  def test_durations_fall_back_to_the_default
    d = Toggleslash::Client
    assert_equal 30.0, d.duration(0, 30.0)
    assert_equal 30.0, d.duration(-1, 30.0)
    assert_equal 30.0, d.duration("5", 30.0)
    assert_equal 30.0, d.duration(Float::NAN, 30.0)
    assert_equal 30.0, d.duration(Float::INFINITY, 30.0)
    assert_equal 30.0, d.duration(nil, 30.0)
    assert_equal 0.25, d.duration(0.25, 30.0)
    assert_equal 31_536_000.0, d.duration(10**12, 30.0)
  end

  def test_close_is_safe_twice_and_concurrent_and_the_client_answers_after
    _server, client = ready_client
    threads = Array.new(4) { Thread.new { client.close } }
    threads.each(&:join)
    client.close
    assert_equal "closed", client.status.stream
    assert_equal true, client.variation("on", { "key" => "u" }, false)
  end

  def test_a_context_attribute_of_each_numeric_type_matches_as_its_value
    rule = { "clauses" => [{ "attribute" => "n", "op" => "in", "values" => [5] }], "variation" => 1 }
    server = build_server
    server.serve_ruleset(FakeServer.ruleset(FakeServer.flag("n", [false, true], 0, rules: [rule])))
    client = build_client(server)
    assert client.ready
    numbers = [5, 5.0, Rational(5), Rational(10, 2), (2**3) - 3, 5.0.to_r, Float(5)]
    begin
      require "bigdecimal"
      numbers << BigDecimal("5")
    rescue LoadError
      nil
    end
    numbers.each do |number|
      assert_equal true, client.variation("n", { "key" => "u", "attributes" => { "n" => number } }, false),
                   number.inspect
    end
    assert_equal false, client.variation("n", { "key" => "u", "attributes" => { "n" => 6 } }, true)
    assert_equal false, client.variation("n", { "key" => "u", "attributes" => { "n" => Complex(5, 1) } }, true)
  end

  def test_a_fractional_numeric_attribute_keeps_its_value
    rule = { "clauses" => [{ "attribute" => "n", "op" => "gt", "values" => [0.5] }], "variation" => 1 }
    server = build_server
    server.serve_ruleset(FakeServer.ruleset(FakeServer.flag("n", [false, true], 0, rules: [rule])))
    client = build_client(server)
    assert client.ready
    assert_equal true, client.variation("n", { "key" => "u", "attributes" => { "n" => Rational(3, 4) } }, false)
    assert_equal false, client.variation("n", { "key" => "u", "attributes" => { "n" => Rational(1, 4) } }, true)
  end
end
