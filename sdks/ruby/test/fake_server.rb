# frozen_string_literal: true

require "socket"
require "openssl"

# A ToggleSlash server on a loopback port, for the SDK's own tests. A test registers a handler per
# (method, path); the handler gets the request and the socket and returns a Hash to answer in one piece
# (`status`, `headers`, `body`), or nil when it wrote the answer itself (a stream, or nothing at all).
class FakeServer
  Request = Struct.new(:verb, :path, :headers, :body)

  def self.flag(key, variations, serve, version: 1, rules: [])
    { "key" => key, "on" => true, "variations" => variations, "offVariation" => 0, "salt" => "salt-#{key}",
      "version" => version, "targets" => [], "prerequisites" => [], "rules" => rules,
      "fallthrough" => { "variation" => serve } }
  end

  def self.ruleset(*flags)
    Toggleslash::Json.generate({ "flags" => flags, "segments" => [] })
  end

  # on: true (index 1 of [false, true]); name: "b"; num: 1.5; big: 2^63, beyond signed 64 bits.
  RULESET = ruleset(flag("on", [false, true], 1, version: 3), flag("name", %w[a b], 1),
                    flag("num", [1.5, 2], 0, version: 2), flag("big", [9_223_372_036_854_775_808, 1], 0)).freeze

  attr_reader :port, :requests

  def initialize(ssl_context: nil)
    @tcp = TCPServer.new("127.0.0.1", 0)
    @server = ssl_context ? OpenSSL::SSL::SSLServer.new(@tcp, ssl_context) : @tcp
    @tls = !ssl_context.nil?
    @port = @tcp.addr[1]
    @handlers = {}
    @requests = []
    @lock = Mutex.new
    @sockets = []
    @stopped = false
    @thread = Thread.new { accept_loop }
    @thread.report_on_exception = false
  end

  def url
    "#{@tls ? "https" : "http"}://127.0.0.1:#{@port}"
  end

  def route(method, path, &handler)
    @lock.synchronize { @handlers["#{method} #{path}"] = handler }
  end

  def requests_to(path)
    @lock.synchronize { @requests.select { |request| request.path == path } }
  end

  def stop
    @stopped = true
    begin
      @tcp.close
    rescue IOError
      nil
    end
    @lock.synchronize { @sockets.dup }.each do |socket|
      socket.close
    rescue IOError, SystemCallError, OpenSSL::SSL::SSLError
      nil
    end
  end

  # Serves a ruleset with a fixed ETag on the ruleset route, 304 when If-None-Match names it.
  def serve_ruleset(body = RULESET, etag: '"v1"')
    route("GET", "/sdk/v1/ruleset") do |request, _socket|
      if request.headers["if-none-match"] == etag
        { status: 304, headers: { "ETag" => etag } }
      else
        { status: 200, headers: { "ETag" => etag, "Content-Type" => "application/json" }, body: body }
      end
    end
  end

  def self.head(socket, status, headers = {})
    lines = headers.map { |name, value| "#{name}: #{value}\r\n" }.join
    socket.write("HTTP/1.1 #{status} X\r\n#{lines}Connection: close\r\n\r\n")
  end

  # The head of an event stream; `chunk` writes one HTTP chunk.
  def self.sse_head(socket)
    head(socket, 200, "Content-Type" => "text/event-stream", "Transfer-Encoding" => "chunked")
  end

  def self.chunk(socket, data)
    socket.write("#{data.bytesize.to_s(16)}\r\n#{data}\r\n")
  end

  private

  def accept_loop
    until @stopped
      socket = @server.accept
      @lock.synchronize { @sockets << socket }
      thread = Thread.new { serve(socket) }
      thread.report_on_exception = false
    end
  rescue IOError, SystemCallError, OpenSSL::SSL::SSLError
    nil
  end

  def serve(socket)
    request = read_request(socket)
    return unless request

    @lock.synchronize { @requests << request }
    handler = @lock.synchronize { @handlers["#{request.verb} #{request.path}"] }
    answer = handler ? handler.call(request, socket) : { status: 404 }
    write_answer(socket, answer) if answer.is_a?(Hash)
  rescue IOError, SystemCallError, OpenSSL::SSL::SSLError
    nil
  ensure
    begin
      socket.close unless socket.closed?
    rescue IOError, SystemCallError, OpenSSL::SSL::SSLError
      nil
    end
  end

  def read_request(socket)
    head = +""
    head << socket.readpartial(65_536) until head.include?("\r\n\r\n")
    head, rest = head.split("\r\n\r\n", 2)
    lines = head.split("\r\n")
    method, path, = lines.first.split
    headers = lines.drop(1).to_h { |line| line.split(":", 2).then { |name, value| [name.downcase, value.strip] } }
    length = headers["content-length"].to_i
    rest << socket.readpartial(length - rest.bytesize) while rest.bytesize < length
    Request.new(method, path, headers, rest)
  rescue EOFError
    nil
  end

  def write_answer(socket, answer)
    body = answer[:body].to_s
    headers = { "Content-Length" => body.bytesize }.merge(answer[:headers] || {})
    FakeServer.head(socket, answer[:status] || 200, headers)
    socket.write(body)
  end
end
