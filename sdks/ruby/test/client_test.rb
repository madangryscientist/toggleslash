# frozen_string_literal: true

require 'timeout'
require_relative 'test_support'

class ClientTest < Minitest::Test
  include ClientSupport

  def test_loads_a_ruleset_and_evaluates_locally
    client = build
    assert client.ready
    assert_equal true, client.bool_variation('f', CONTEXT, false)
    assert_equal server.etag, client.status.etag
    assert_equal :off, client.status.stream
  end

  def test_evaluating_before_the_first_load_is_not_ready_at_once
    server.on('/sdk/v1/ruleset') { |_request, _io| sleep 5 }
    client = build
    started = ToggleSlash::Clock.monotonic
    detail = client.variation_detail('f', CONTEXT, false)
    assert_operator ToggleSlash::Clock.monotonic - started, :<, 0.5
    assert_equal :NOT_READY, detail.reason.code
    assert_equal false, detail.value
  end

  def test_a_second_poll_sends_if_none_match_and_a_304_is_a_sync
    client = build(poll_interval: 0.05)
    assert client.ready
    wait_until(5, 'a 304 poll') { server.requests_to('/sdk/v1/ruleset').size >= 3 }
    assert_equal server.etag, server.requests_to('/sdk/v1/ruleset')[2].headers['if-none-match']
    assert_nil client.status.last_error
  end

  def test_operations_called_from_many_threads_with_close_racing
    client = build(poll_interval: 0.02, flush_interval: 0.02)
    assert client.ready
    stop = false
    errors = Queue.new
    workers = Array.new(12) do |n|
      Thread.new do
        until stop
          client.variation('f', CONTEXT, false)
          client.int_variation_detail('f', CONTEXT, 0)
          client.status
          client.ready
          client.close if n.zero?
          Thread.pass
        end
      rescue StandardError, ScriptError => e
        errors << e
      end
    end
    sleep 0.3
    closers = Array.new(4) { Thread.new { Timeout.timeout(5) { client.close } } }
    closers.each { |closer| closer.join(6) || flunk('close did not return') }
    stop = true
    workers.each { |worker| worker.join(5) || flunk('a worker did not stop') }
    assert_empty Array.new(errors.size) { errors.pop }
    assert_equal :closed, client.status.stream
  end

  def test_a_close_from_on_error_on_the_sdk_threads_and_the_closing_thread_does_not_deadlock
    server.on('/sdk/v1/ruleset') { |_request, io| FakeServer.respond(io, 500) }
    holder = []
    closed = Queue.new
    on_error = lambda do |_error|
      holder.first.close
      closed << true
    end
    client = build(on_error: on_error, poll_interval: 0.05, flush_interval: 0.05)
    holder << client
    Timeout.timeout(5) { closed.pop }
    Timeout.timeout(5) { client.close }
    assert_equal :closed, client.status.stream
  end

  def test_close_waits_for_a_hung_usage_flush_for_at_most_the_request_timeout
    server.on('/sdk/v1/events') { |_request, _io| sleep 5 }
    client = build(flush_interval: 0.05, request_timeout: 0.5)
    assert client.ready
    client.variation('f', CONTEXT, false)
    wait_until(5, 'the flush') { server.requests_to('/sdk/v1/events').any? }
    started = ToggleSlash::Clock.monotonic
    client.close
    assert_operator ToggleSlash::Clock.monotonic - started, :<, 3
  end

  def test_variation_returns_while_a_ruleset_request_hangs
    client = build
    assert client.ready
    server.on('/sdk/v1/ruleset') { |_request, _io| sleep 5 }
    server.publish([server.flag('f', false, 2)])
    sleep 0.2
    assert_returns_quickly { client.variation('f', CONTEXT, false) }
  end

  def test_variation_returns_while_a_usage_flush_hangs
    server.on('/sdk/v1/events') { |_request, _io| sleep 5 }
    client = build(flush_interval: 0.05)
    assert client.ready
    client.variation('f', CONTEXT, false)
    wait_until(5, 'the flush') { server.requests_to('/sdk/v1/events').any? }
    assert_returns_quickly { client.variation_detail('f', CONTEXT, false) }
  end

  def test_a_hook_sees_an_etag_and_flag_version_of_the_same_ruleset_across_swaps
    seen = Queue.new
    client = build(poll_interval: 0.01, hooks: { after_evaluation: ->(event) { seen << event } })
    assert client.ready
    publisher = Thread.new do
      2.upto(60) do |n|
        server.publish([server.flag('f', true, n)])
        sleep 0.01
      end
    end
    reader = Thread.new do
      while publisher.alive?
        client.variation('f', CONTEXT, false)
        Thread.pass
      end
    end
    [publisher, reader].each(&:join)
    events = Array.new(seen.size) { seen.pop }
    refute_empty events
    events.each do |event|
      assert_equal %("v#{event.flag_version}"), event.etag, 'the etag and the flag version come from two rulesets'
    end
    assert_operator events.map(&:flag_version).uniq.size, :>, 1
  end

  def test_a_hook_and_on_error_that_call_back_into_the_client_do_not_deadlock
    holder = []
    reentered = false
    hook = lambda do |_event|
      next if reentered

      reentered = true
      holder.first.status
      holder.first.variation('f', CONTEXT, false)
      raise 'the hook raised'
    end
    reported = Queue.new
    on_error = lambda do |error|
      holder.first.status
      holder.first.variation('f', CONTEXT, false)
      reported << error.message
    end
    client = build(hooks: { after_evaluation: hook }, on_error: on_error)
    holder << client
    assert client.ready
    Timeout.timeout(5) do
      assert_equal true, client.variation('f', CONTEXT, false)
      assert_equal 'the hook raised', reported.pop
    end
  end

  def test_an_evaluation_that_raises_gives_the_fallback_and_one_on_error
    failing = Object.new
    def failing.evaluate(*)
      raise 'the evaluator broke'
    end
    errors = []
    client = build(evaluator: failing, on_error: ->(error) { errors << error.message })
    assert client.ready
    detail = client.variation_detail('f', CONTEXT, 'fallback')
    assert_equal 'fallback', detail.value
    assert_nil detail.variation_index
    assert_equal :ERROR, detail.reason.kind
    assert_equal :EVALUATION_FAILED, detail.reason.code
    assert_equal ['the evaluator broke'], errors
  end

  def test_an_on_error_that_raises_is_ignored
    failing = Object.new
    def failing.evaluate(*)
      raise 'the evaluator broke'
    end
    client = build(evaluator: failing, on_error: ->(_error) { raise 'on_error broke' })
    assert client.ready
    assert_equal :EVALUATION_FAILED, client.variation_detail('f', CONTEXT, 1).reason.code
  end

  def test_a_hook_that_is_not_callable_is_ignored
    client = build(hooks: { after_evaluation: 'nope' })
    assert client.ready
    assert_equal true, client.variation('f', CONTEXT, false)
  end

  def test_the_integer_accessor_follows_the_signed_64_bit_rule
    flags = [['whole', 2.0], ['half', 1.5], ['big', 2**63], ['edge', -(2**63)], ['top', (2**63) - 1],
             ['huge', 1e30], ['many', 12_345_678_901_234_567_890]]
    server.publish(flags.map { |key, value| server.flag(key, value, 1) })
    client = build
    assert client.ready
    expected = { 'whole' => 2, 'half' => :mismatch, 'big' => :mismatch, 'edge' => -(2**63), 'top' => :mismatch_or_max,
                 'huge' => :mismatch, 'many' => :mismatch }
    expected.each do |key, want|
      detail = client.int_variation_detail(key, CONTEXT, 0)
      case want
      when :mismatch then assert_equal :TYPE_MISMATCH, detail.reason.code, key
      when :mismatch_or_max then assert_includes [:TYPE_MISMATCH, nil], detail.reason.code
      else
        assert_equal want, detail.value, key
        assert_kind_of Integer, detail.value
      end
    end
  end

  def test_a_nil_fallback_accepts_any_kind_and_other_kinds_mismatch
    client = build
    assert client.ready
    assert_equal true, client.variation('f', CONTEXT, nil)
    assert_equal :TYPE_MISMATCH, client.variation_detail('f', CONTEXT, 'x').reason.code
    assert_equal :TYPE_MISMATCH, client.number_variation_detail('f', CONTEXT, 0).reason.code
    assert_equal :TYPE_MISMATCH, client.json_variation_detail('f', CONTEXT, {}).reason.code
  end

  def test_counts_either_side_of_a_utc_midnight_go_out_as_two_days
    clock = ClientSupport::FakeClock.new(Time.utc(2026, 11, 10, 23, 59, 59))
    client = build(clock: clock)
    assert client.ready
    3.times { client.variation('f', CONTEXT, false) }
    clock.time = Time.utc(2026, 11, 11, 0, 0, 1)
    2.times { client.variation('f', CONTEXT, false) }
    client.close
    body = ToggleSlash::Json.parse(server.requests_to('/sdk/v1/events').first.body)
    days = body['events'].to_h { |entry| [entry['day'], entry['count']] }
    assert_equal({ '2026-11-10' => 3, '2026-11-11' => 2 }, days)
  end

  def test_nothing_is_counted_after_close_begins_and_close_flushes
    client = build
    assert client.ready
    client.variation('f', CONTEXT, false)
    client.close
    client.variation('f', CONTEXT, false)
    assert_equal true, client.variation('f', CONTEXT, false), 'evaluation still answers after close'
    assert_equal 1, ToggleSlash::Json.parse(server.requests_to('/sdk/v1/events').first.body)['events'].first['count']
    assert_equal :closed, client.status.stream
  end

  def test_a_batch_holds_at_most_500_entries_and_usage_carries_no_context
    flags = Array.new(600) { |n| server.flag("flag#{n}", true, 1) }
    server.publish(flags)
    client = build
    assert client.ready
    600.times { |n| client.variation("flag#{n}", { 'key' => 'secret-user', 'plan' => 'secret-plan' }, false) }
    client.close
    posts = server.requests_to('/sdk/v1/events')
    assert_equal([500, 100], posts.map { |post| ToggleSlash::Json.parse(post.body)['events'].size })
    posts.each do |post|
      refute_includes post.body, 'secret'
      assert_equal 'application/json', post.headers['content-type']
    end
  end

  def test_a_retry_after_above_300_is_held_for_300_seconds_on_the_ruleset_route
    server.on('/sdk/v1/ruleset') { |_request, io| FakeServer.respond(io, 429, 'Retry-After' => '99999') }
    client = build
    refute client.ready
    assert_equal :http_429, client.status.last_error
    assert_in_delta 300, client.hold_remaining(:sync), 5
  end

  def test_a_retry_after_above_300_is_held_for_300_seconds_on_the_stream_route
    server.on('/sdk/v1/stream') { |_request, io| FakeServer.respond(io, 429, 'Retry-After' => '99999') }
    client = build(stream: true)
    assert client.ready
    wait_until(5, 'the stream 429') { client.status.last_error == :http_429 }
    assert_in_delta 300, client.hold_remaining(:sync), 5
    assert_in_delta 0, client.hold_remaining(:usage), 0.001
  end

  def test_the_events_route_has_its_own_hold_capped_at_300
    server.on('/sdk/v1/events') { |_request, io| FakeServer.respond(io, 429, 'Retry-After' => '99999') }
    errors = []
    client = build(flush_interval: 0.05, on_error: ->(error) { errors << error.message })
    assert client.ready
    client.variation('f', CONTEXT, false)
    wait_until(5, 'the 429') { server.requests_to('/sdk/v1/events').any? }
    wait_until(5, 'the hold') { client.hold_remaining(:usage).positive? }
    assert_in_delta 300, client.hold_remaining(:usage), 5
    assert_in_delta 0, client.hold_remaining(:sync), 0.001
    client.variation('f', CONTEXT, false)
    client.close
    assert_equal 1, server.requests_to('/sdk/v1/events').size
    assert(errors.any? { |message| message.include?('closed with 1 usage counts unsent: the server asked to wait') })
  end

  def test_a_client_that_is_never_closed_does_not_keep_the_process_alive
    script = <<~RUBY
      require 'toggleslash'
      ToggleSlash.create(url: 'http://127.0.0.1:#{server.port}', sdk_key: 'k', stream: true)
      sleep 0.2
    RUBY
    started = ToggleSlash::Clock.monotonic
    pid = Process.spawn(RbConfig.ruby, '-w', '-I', File.expand_path('../lib', __dir__), '-e', script)
    _pid, exit_status = Timeout.timeout(10) { Process.wait2(pid) }
    assert_predicate exit_status, :success?
    assert_operator ToggleSlash::Clock.monotonic - started, :<, 8
  end

  def test_unknown_and_bad_options_take_defaults
    client = ToggleSlash.create(url: server.url, sdk_key: 'k', stream: false, poll_interval: -1, bogus: 1,
                                request_timeout: 'x', hooks: 3, on_error: 7)
    clients << client
    assert client.ready
    assert_equal true, client.variation('f', CONTEXT, false)
  end

  def test_a_url_that_cannot_be_reached_never_raises
    client = ToggleSlash.create(url: 'not a url', sdk_key: 'k')
    clients << client
    refute client.ready
    assert_equal :network, client.status.last_error
    assert_equal 5, client.variation('f', CONTEXT, 5)
  end

  private

  def assert_returns_quickly
    started = ToggleSlash::Clock.monotonic
    yield
    assert_operator ToggleSlash::Clock.monotonic - started, :<, 0.5
  end
end
