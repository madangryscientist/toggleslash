# frozen_string_literal: true

require 'socket'

# A ToggleSlash server on a loopback port, as far as the SDK's tests need one. It serves a ruleset with an ETag
# (and 304 on a matching If-None-Match), takes usage counts, and answers the stream route 503 until a test
# gives it a handler. A test replaces any route's behaviour with `on(path) { |request, io| ... }`.
class FakeServer
  Request = Struct.new(:method, :path, :headers, :body, :at)

  attr_reader :port, :requests

  def initialize
    @server = TCPServer.new('127.0.0.1', 0)
    @port = @server.addr[1]
    @lock = Mutex.new
    @requests = []
    @handlers = {}
    @sockets = []
    @etag_number = 1
    @flags = [flag('f', true, 1)]
    @stop = false
    @acceptor = Thread.new { accept_loop }
    @acceptor.report_on_exception = false
  end

  def url
    "http://127.0.0.1:#{@port}"
  end

  def flag(key, value, version)
    { 'key' => key, 'variations' => [false, true, value], 'on' => true, 'offVariation' => 0, 'targets' => [],
      'prerequisites' => [], 'rules' => [], 'fallthrough' => { 'variation' => 2 }, 'salt' => 's',
      'version' => version }
  end

  # Replaces the ruleset and bumps the ETag, which is `"v<n>"`.
  def publish(flags)
    @lock.synchronize do
      @etag_number += 1
      @flags = flags
    end
  end

  def etag
    @lock.synchronize { %("v#{@etag_number}") }
  end

  def on(path, &handler)
    @lock.synchronize { @handlers[path] = handler }
  end

  def requests_to(path)
    @lock.synchronize { @requests.select { |request| request.path == path } }
  end

  def stop
    @stop = true
    @server.close unless @server.closed?
    @lock.synchronize { @sockets.dup }.each { |socket| socket.close unless socket.closed? }
  rescue IOError, SystemCallError
    nil
  end

  # Writes a complete answer with a Content-Length.
  def self.respond(io, status, headers = {}, body = '')
    lines = ["HTTP/1.1 #{status} X", "Content-Length: #{body.bytesize}", 'Connection: close']
    headers.each { |name, value| lines << "#{name}: #{value}" }
    io.write("#{lines.join("\r\n")}\r\n\r\n#{body}")
  end

  # Starts a chunked event stream and returns; the caller writes events with `event`.
  def self.open_stream(io)
    io.write("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n" \
             "Connection: close\r\n\r\n")
  end

  def self.event(io, text)
    io.write("#{text.bytesize.to_s(16)}\r\n#{text}\r\n")
  end

  private

  def accept_loop
    until @stop
      socket = @server.accept
      @lock.synchronize { @sockets << socket }
      Thread.new(socket) do |connection|
        Thread.current.report_on_exception = false
        serve(connection)
      end
    end
  rescue IOError, SystemCallError
    nil
  end

  def serve(socket)
    request = read(socket)
    return if request.nil?

    @lock.synchronize { @requests << request }
    handler = @lock.synchronize { @handlers[request.path] }
    handler ? handler.call(request, socket) : default(request, socket)
  rescue IOError, SystemCallError
    nil
  ensure
    socket.close unless socket.closed?
  end

  def read(socket)
    line = socket.gets("\r\n")&.chomp
    return nil if line.nil?

    method, path = line.split
    headers = {}
    while (header = socket.gets("\r\n")) && header != "\r\n"
      name, value = header.chomp.split(':', 2)
      headers[name.downcase] = value.to_s.strip
    end
    length = headers['content-length'].to_i
    Request.new(method, path, headers, length.positive? ? socket.read(length) : '', Time.now)
  end

  def default(request, socket)
    case request.path
    when '/sdk/v1/ruleset' then ruleset(request, socket)
    when '/sdk/v1/events' then self.class.respond(socket, 200)
    else self.class.respond(socket, 503)
    end
  end

  def ruleset(request, socket)
    tag, flags = @lock.synchronize { [%("v#{@etag_number}"), @flags] }
    if request.headers['if-none-match'] == tag
      self.class.respond(socket, 304, 'ETag' => tag)
    else
      self.class.respond(socket, 200, { 'ETag' => tag, 'Content-Type' => 'application/json' },
                         ToggleSlash::Json.generate({ 'flags' => flags, 'segments' => [] }))
    end
  end
end
