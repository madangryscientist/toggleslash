# frozen_string_literal: true

require 'timeout'
require_relative 'test_support'

class HttpTest < Minitest::Test
  include ClientSupport

  def request(path, cancel: nil, seconds: 2, headers: {}, body: nil, method: 'GET')
    target = ToggleSlash::Http.target(server.url)
    ToggleSlash::Http.request(target, method, path, deadline: ToggleSlash::Clock.monotonic + seconds,
                                                    cancel: cancel, headers: headers, body: body)
  end

  def test_reads_a_content_length_body_and_lowercases_header_names
    server.on('/a') { |_request, io| FakeServer.respond(io, 200, { 'X-Thing' => 'v' }, 'hello') }
    response = request('/a')
    assert_equal 200, response.status
    assert_equal 'v', response.header('x-thing')
    assert_equal 'hello', response.read_body(ToggleSlash::Clock.monotonic + 2)
  ensure
    response&.close
  end

  def test_reads_a_chunked_body_split_across_writes
    server.on('/c') do |_request, io|
      io.write("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n")
      io.write("3\r\nab")
      sleep 0.05
      io.write("c\r\n2\r\nde\r\n0\r\n\r\n")
    end
    response = request('/c')
    assert_equal 'abcde', response.read_body(ToggleSlash::Clock.monotonic + 2)
  ensure
    response&.close
  end

  def test_the_deadline_covers_the_body
    server.on('/slow') do |_request, io|
      io.write("HTTP/1.1 200 OK\r\nContent-Length: 10\r\nConnection: close\r\n\r\nabc")
      sleep 5
    end
    response = request('/slow', seconds: 0.4)
    assert_raises(ToggleSlash::Http::Timeout) { response.read_body(ToggleSlash::Clock.monotonic + 0.4) }
  ensure
    response&.close
  end

  def test_a_server_that_never_answers_times_out
    server.on('/hang') { |_request, _io| sleep 5 }
    assert_raises(ToggleSlash::Http::Timeout) { request('/hang', seconds: 0.3) }
  end

  def test_a_cancel_abandons_a_request_at_once
    server.on('/hang') { |_request, _io| sleep 5 }
    cancel = ToggleSlash::Cancel.new
    Thread.new do
      sleep 0.2
      cancel.cancel
    end
    started = ToggleSlash::Clock.monotonic
    assert_raises(ToggleSlash::Http::Cancelled) { request('/hang', cancel: cancel, seconds: 5) }
    assert_operator ToggleSlash::Clock.monotonic - started, :<, 1
  ensure
    cancel&.release
  end

  def test_a_refused_connection_is_a_network_error
    port = server.port
    server.stop
    target = ToggleSlash::Http.target("http://127.0.0.1:#{port}")
    assert_raises(ToggleSlash::Http::Network) do
      ToggleSlash::Http.request(target, 'GET', '/', deadline: ToggleSlash::Clock.monotonic + 2)
    end
  end

  def test_a_header_value_with_a_line_break_is_refused
    assert_raises(ToggleSlash::Http::Network) { request('/a', headers: { 'X' => "a\r\nb: c" }) }
  end

  def test_a_post_sends_its_body_with_a_content_length
    server.on('/p') { |_request, io| FakeServer.respond(io, 200) }
    request('/p', method: 'POST', body: '{"a":1}', headers: { 'Content-Type' => 'application/json' }).close
    sent = server.requests_to('/p').first
    assert_equal '{"a":1}', sent.body
    assert_equal '7', sent.headers['content-length']
  end

  def test_the_headers_the_client_adds_are_the_protocol_ones_and_nothing_that_names_a_user
    server.on('/h') { |_request, io| FakeServer.respond(io, 200) }
    request('/h', headers: { 'Authorization' => 'Bearer k', 'Accept' => 'application/json' }).close
    assert_equal %w[accept authorization connection host], server.requests_to('/h').first.headers.keys.sort
  end

  def test_retry_after_counts_on_a_429_only_in_whole_seconds_capped_at_300
    assert_equal 12, ToggleSlash::Http.retry_after(429, ' 12 ')
    assert_equal 300, ToggleSlash::Http.retry_after(429, '99999')
    assert_nil ToggleSlash::Http.retry_after(429, 'Wed, 21 Oct 2026 07:28:00 GMT')
    assert_nil ToggleSlash::Http.retry_after(503, '12')
    assert_nil ToggleSlash::Http.retry_after(429, nil)
  end

  def test_target_joins_a_base_path_and_refuses_what_is_not_http
    target = ToggleSlash::Http.target('http://127.0.0.1:9/base')
    assert_equal '/base', target.prefix
    assert_nil ToggleSlash::Http.target('ftp://x')
    assert_nil ToggleSlash::Http.target('')
  end
end
