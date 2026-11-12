# frozen_string_literal: true

require_relative "support"
require "timeout"

class HttpTest < Minitest::Test
  include Support

  Http = Toggleslash::Http

  def fetch(server, path = "/x", timeout: 2.0, headers: {})
    transport = Http::Transport.new
    transport.fetch(Http::Endpoint.new(server.url), "GET", path, headers, nil, timeout, nil)
  end

  def raw(server, path, &write)
    server.route("GET", path) do |_request, socket|
      write.call(socket)
      nil
    end
  end

  def test_content_length_body
    server = build_server
    server.route("GET", "/x") { { status: 200, body: "hello", headers: { "X-One" => "1" } } }
    response = fetch(server)
    assert_equal [200, "hello", "1"], [response.status, response.body, response.header("x-one")]
  end

  def test_chunked_body_with_extensions_and_trailers
    server = build_server
    raw(server, "/x") do |socket|
      FakeServer.head(socket, 200, "Transfer-Encoding" => "chunked")
      socket.write("5;ext=1\r\nhello\r\n3\r\n wo\r\n0\r\nTrailer: x\r\n\r\n")
    end
    assert_equal "hello wo", fetch(server).body
  end

  def test_a_body_read_to_close_when_no_length_is_given
    server = build_server
    raw(server, "/x") do |socket|
      FakeServer.head(socket, 200)
      socket.write("until close")
    end
    assert_equal "until close", fetch(server).body
  end

  def test_a_truncated_body_is_a_failure_and_not_a_short_success
    server = build_server
    raw(server, "/x") do |socket|
      FakeServer.head(socket, 200, "Content-Length" => 100)
      socket.write("short")
    end
    assert_raises(Http::NetworkFailure) { fetch(server) }
  end

  def test_a_truncated_chunked_body_is_a_failure
    server = build_server
    raw(server, "/x") do |socket|
      FakeServer.head(socket, 200, "Transfer-Encoding" => "chunked")
      socket.write("5\r\nhel")
    end
    assert_raises(Http::NetworkFailure) { fetch(server) }
  end

  def test_an_interim_100_answer_is_skipped
    server = build_server
    raw(server, "/x") do |socket|
      socket.write("HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
    end
    response = fetch(server)
    assert_equal [200, "ok"], [response.status, response.body]
  end

  def test_a_redirect_is_returned_not_followed
    server = build_server
    server.route("GET", "/x") { { status: 302, headers: { "Location" => "/y" } } }
    assert_equal 302, fetch(server).status
    assert_empty server.requests_to("/y")
  end

  def test_a_response_head_cut_off_is_a_failure
    server = build_server
    raw(server, "/x") { |socket| socket.write("HTTP/1.1 200 OK\r\n#{"X: #{"a" * 100}\r\n" * 1000}") }
    assert_raises(Http::NetworkFailure) { fetch(server) }
  end

  def test_garbage_is_a_failure
    server = build_server
    raw(server, "/x") { |socket| socket.write("not http at all\r\n\r\n") }
    assert_raises(Http::NetworkFailure) { fetch(server) }
  end

  def test_a_header_with_a_line_break_is_refused
    server = build_server
    server.route("GET", "/x") { { status: 200 } }
    assert_raises(Http::NetworkFailure) { fetch(server, headers: { "X-Bad" => "a\r\nInjected: 1" }) }
    assert_empty server.requests
  end

  def test_a_path_prefix_in_the_url_is_kept
    server = build_server
    server.route("GET", "/prefix/sdk/v1/ruleset") { { status: 200, body: "p" } }
    transport = Http::Transport.new
    response = transport.fetch(Http::Endpoint.new("#{server.url}/prefix"), "GET", "/sdk/v1/ruleset", {}, nil, 2.0, nil)
    assert_equal "p", response.body
  end

  def test_a_bad_url_is_a_failure_and_never_raises_another_class
    ["", "ftp://x", "http://", "http://127.0.0.1:99999", "not a url"].each do |url|
      endpoint = Http::Endpoint.new(url)
      refute endpoint.valid?, url
      assert_raises(Http::NetworkFailure) { Http::Transport.new.fetch(endpoint, "GET", "/x", {}, nil, 1.0, nil) }
    end
  end

  def test_the_timeout_covers_the_whole_request
    server = build_server
    raw(server, "/x") do |socket|
      FakeServer.head(socket, 200, "Content-Length" => 10)
      5.times do
        socket.write("a")
        sleep 0.2
      end
    end
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    assert_raises(Http::RequestTimeout) { fetch(server, timeout: 0.4) }
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 1.0
  end

  def test_abort_wakes_a_waiting_request
    server = build_server
    raw(server, "/x") { |socket| sleep 0.01 until socket.closed? }
    aborts = Http::Aborts.new
    thread = Thread.new do
      Http::Transport.new.fetch(Http::Endpoint.new(server.url), "GET", "/x", {}, nil, 10.0, aborts)
    rescue Http::Aborted
      :aborted
    end
    sleep 0.2
    aborts.abort_all
    assert_equal :aborted, Timeout.timeout(2) { thread.value }
    aborts.dispose
  end

  def test_a_stream_reads_until_the_server_ends
    server = build_server
    raw(server, "/s") do |socket|
      FakeServer.sse_head(socket)
      FakeServer.chunk(socket, "one")
      FakeServer.chunk(socket, "two")
      socket.write("0\r\n\r\n")
    end
    transport = Http::Transport.new
    stream = transport.open_stream(Http::Endpoint.new(server.url), "/s", {}, 2.0, 2.0, nil)
    assert_equal 200, stream.status
    got = +""
    while (chunk = stream.read)
      got << chunk
    end
    assert_equal "onetwo", got
    stream.close
  end

  def test_a_stream_idle_longer_than_the_bound_times_out
    server = build_server
    raw(server, "/s") do |socket|
      FakeServer.sse_head(socket)
      sleep 0.01 until socket.closed?
    end
    stream = Http::Transport.new.open_stream(Http::Endpoint.new(server.url), "/s", {}, 2.0, 0.2, nil)
    assert_raises(Http::RequestTimeout) { stream.read }
    stream.close
  end
end
