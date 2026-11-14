# frozen_string_literal: true

# The Ruby SDK's conformance test service: an HTTP server that wraps `Toggleslash.create` so the harness in
# `packages/sdk-conformance` can drive it. The protocol is docs/sdk-test-service.md; the accessor mapping is
# in this directory's README. It rescues nothing the SDK raises: a call that raises answers 500 with the
# error text, and the harness fails the scenario. The server is a few lines over `TCPServer` because the
# SDK has no runtime dependency and neither does its service.

$LOAD_PATH.unshift(File.expand_path("../lib", __dir__))
require "socket"
require "toggleslash"

# One client the harness created, with the hook events and errors it asked to record.
class Entry
  attr_accessor :client
  attr_reader :hook_events, :errors, :lock

  def initialize
    @client = nil
    @hook_events = []
    @errors = []
    @lock = Mutex.new
    # closeInHook / closeInOnError: the close made from inside a callback. `closing` is set when it starts,
    # `calls` once it has returned.
    @closing = false
    @callback_close = { "calls" => 0, "threw" => 0, "maxMs" => 0 }
  end

  def callback_close
    @lock.synchronize { @callback_close.dup }
  end

  # The first callback that finds the client stored closes it, timed; anything close raises is caught.
  def close_from_callback
    client = @lock.synchronize do
      next nil if @client.nil? || @closing

      @closing = true
      @client
    end
    return if client.nil?

    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    threw = 0
    begin
      client.close
    rescue StandardError
      threw = 1
    end
    took = ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - started) * 1000).round
    @lock.synchronize { @callback_close = { "calls" => 1, "threw" => threw, "maxMs" => took } }
  end
end

# The routes of docs/sdk-test-service.md.
class Service
  def initialize
    @clients = {}
    @lock = Mutex.new
    @counter = 0
  end

  def handle(method, path, body)
    path = path.split("?", 2).first
    if method == "GET" && path == "/"
      return [200,
              { "name" => "toggleslash-sdk (Ruby)",
                "capabilities" => ["integer-accessor"] }]
    end
    return [201, create(parse(body))] if method == "POST" && path == "/clients"

    match = %r{\A/clients/([^/]+)/([a-z-]+)\z}.match(path)
    entry = match && @lock.synchronize { @clients[match[1]] }
    return [404, "no such client"] unless entry&.client

    client(entry, "#{method} #{match[2]}", body)
  end

  private

  def parse(body)
    return {} if body.nil? || body.empty?

    parsed = Toggleslash::Json.parse(body)
    raise TypeError, "the body is not a JSON object" unless parsed.is_a?(Hash)

    parsed
  end

  def client(entry, action, body)
    client = entry.client
    case action
    when "POST ready" then [200, { "ready" => client.ready }]
    when "POST evaluate" then [200, evaluate(client, parse(body))]
    when "GET status" then [200, status_json(client.status)]
    when "GET hook-events" then [200, { "events" => entry.lock.synchronize { entry.hook_events.dup } }]
    when "GET errors" then [200, errors_json(entry)]
    when "GET callback-close" then [200, entry.callback_close]
    when "POST close" then close(client)
    else [404, "no such route"]
    end
  end

  def close(client)
    client.close
    [200, {}]
  end

  def errors_json(entry)
    messages = entry.lock.synchronize { entry.errors.dup }
    { "count" => messages.size, "messages" => messages }
  end

  def create(body)
    entry = Entry.new
    options = { url: body["url"].to_s, sdk_key: body["sdkKey"].to_s, on_error: error_recorder(entry, body) }
    options[:stream] = body["stream"] if [true, false].include?(body["stream"])
    { pollIntervalMs: :poll_interval, flushIntervalMs: :flush_interval, requestTimeoutMs: :request_timeout }
      .each do |name, option|
        value = body[name.to_s]
        options[option] = value / 1000.0 if value.is_a?(Numeric)
      end
    if %w[hook hookThrows closeInHook].any? { |name| body[name] == true }
      options[:hooks] =
        Toggleslash::Hooks.new(after_evaluation: hook_recorder(entry,
                                                               body))
    end
    client = Toggleslash.create(**options)
    entry.lock.synchronize { entry.client = client }
    identifier = @lock.synchronize { (@counter += 1).to_s }
    @lock.synchronize { @clients[identifier] = entry }
    { "id" => identifier }
  end

  def error_recorder(entry, body)
    lambda do |error|
      entry.lock.synchronize { entry.errors << error.message }
      entry.close_from_callback if body["closeInOnError"] == true
      raise "on_error raised, as the test asked" if body["onErrorThrows"] == true
    end
  end

  def hook_recorder(entry, body)
    lambda do |event|
      entry.lock.synchronize { entry.hook_events << event_json(event) }
      entry.close_from_callback if body["closeInHook"] == true
      raise "the hook raised, as the test asked" if body["hookThrows"] == true
    end
  end

  def evaluate(client, body)
    flag = body["flagKey"].to_s
    fallback = body["fallback"]
    detail =
      if body["accessor"] == "int"
        fallback = fallback.to_i if fallback.is_a?(Float) && fallback.finite? && fallback == fallback.floor
        client.int_variation_detail(flag, body["context"], fallback)
      else
        client.variation_detail(flag, body["context"], fallback)
      end
    { "value" => clean(detail.value), "variationIndex" => detail.variation_index,
      "reason" => reason_json(detail.reason) }
  end

  # JSON cannot hold a non-finite number: a JavaScript service would answer `null` for one.
  def clean(value)
    case value
    when Float then value.finite? ? value : nil
    when Array then value.map { |item| clean(item) }
    when Hash then value.to_h { |key, item| [key.to_s, clean(item)] }
    else value
    end
  end

  def reason_json(reason)
    out = { "kind" => reason.kind }
    out["code"] = reason.code unless reason.code.nil?
    out["ruleIndex"] = reason.rule_index unless reason.rule_index.nil?
    out["flagKey"] = reason.flag_key unless reason.flag_key.nil?
    out
  end

  def event_json(event)
    { "flagKey" => event.flag_key, "context" => clean(event.context), "value" => clean(event.value),
      "variationIndex" => event.variation_index, "reason" => reason_json(event.reason),
      "etag" => event.etag, "flagVersion" => event.flag_version }
  end

  def status_json(status)
    at = status.last_sync_at
    { "etag" => status.etag, "lastSyncAt" => at && (at.to_r * 1000).floor, "stream" => status.stream,
      "lastError" => status.last_error }
  end
end

# Reads one request, answers it, closes.
class Connection
  def initialize(socket, service)
    @socket = socket
    @service = service
  end

  def run
    method, path, body = read_request
    status, payload = begin
      @service.handle(method, path, body)
    rescue StandardError => e
      [500, "#{e.class}: #{e.message}\n#{e.backtrace&.first(8)&.join("\n")}"]
    end
    reply(status, payload)
  rescue StandardError
    nil
  ensure
    @socket.close unless @socket.closed?
  end

  private

  def read_request
    head = +""
    head << @socket.readpartial(65_536) until head.include?("\r\n\r\n")
    head, rest = head.b.split("\r\n\r\n", 2)
    lines = head.split("\r\n")
    method, path, = lines.first.split
    length = lines.drop(1).filter_map { |line| line[/\Acontent-length:\s*(\d+)/i, 1] }.first.to_i
    rest << @socket.readpartial(length - rest.bytesize) while rest.bytesize < length
    [method, path, rest]
  end

  def reply(status, payload)
    if payload.is_a?(String)
      type = "text/plain; charset=utf-8"
      data = payload.b
    else
      type = "application/json"
      data = Toggleslash::Json.generate(payload).b
    end
    @socket.write("HTTP/1.1 #{status} X\r\nContent-Type: #{type}\r\nContent-Length: #{data.bytesize}\r\n" \
                  "Connection: close\r\n\r\n".b + data)
  end
end

server = TCPServer.new("127.0.0.1", 0)
service = Service.new
$stdout.write("listening http://127.0.0.1:#{server.addr[1]}\n")
$stdout.flush
loop do
  socket = server.accept
  thread = Thread.new { Connection.new(socket, service).run }
  thread.report_on_exception = false
end
