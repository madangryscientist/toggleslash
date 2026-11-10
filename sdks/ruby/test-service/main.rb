# frozen_string_literal: true

# The conformance test service (docs/sdk-test-service.md): an HTTP server on 127.0.0.1 that wraps
# ToggleSlash.create so the harness in packages/sdk-conformance can drive it from outside. It catches nothing
# the SDK raises: a call that raises answers 500 with the error text, and the harness fails the scenario.
# The accessor mapping is in README.md next to this file.

$LOAD_PATH.unshift(File.expand_path('../lib', __dir__))
require 'socket'
require 'toggleslash'

module TestService
  Entry = Struct.new(:client, :hook_events, :errors, :lock)

  CLIENTS = {} # rubocop:disable Style/MutableConstant
  CLIENTS_LOCK = Mutex.new
  COUNTER = [0] # rubocop:disable Style/MutableConstant

  ACCESSORS = {
    'bool' => :bool_variation_detail, 'string' => :string_variation_detail,
    'number' => :number_variation_detail, 'int' => :int_variation_detail,
    'json' => :json_variation_detail, 'any' => :variation_detail
  }.freeze

  class NotFound < StandardError; end

  module_function

  def options_for(body, entry)
    options = {
      url: body['url'].to_s, sdk_key: body['sdkKey'].to_s,
      on_error: lambda do |error|
        entry.lock.synchronize { entry.errors << error.message.to_s }
        raise 'onError raised, as the test asked' if body['onErrorThrows'] == true
      end
    }
    options[:stream] = body['stream'] if [true, false].include?(body['stream'])
    { 'pollIntervalMs' => :poll_interval, 'flushIntervalMs' => :flush_interval,
      'requestTimeoutMs' => :request_timeout }.each do |name, key|
      options[key] = body[name] / 1000.0 if body[name].is_a?(Numeric)
    end
    options[:hooks] = hooks_for(body, entry) if body['hook'] == true || body['hookThrows'] == true
    options
  end

  def hooks_for(body, entry)
    { after_evaluation: lambda do |event|
      entry.lock.synchronize { entry.hook_events << event_json(event) }
      raise 'the hook raised, as the test asked' if body['hookThrows'] == true
    end }
  end

  def event_json(event)
    { 'flagKey' => event.flag_key, 'context' => event.context, 'value' => event.value,
      'variationIndex' => event.variation_index, 'reason' => event.reason.wire,
      'etag' => event.etag, 'flagVersion' => event.flag_version }
  end

  def create(body)
    entry = Entry.new(nil, [], [], Mutex.new)
    entry.client = ToggleSlash.create(**options_for(body, entry))
    id = CLIENTS_LOCK.synchronize { (COUNTER[0] += 1).to_s }
    CLIENTS_LOCK.synchronize { CLIENTS[id] = entry }
    [201, { 'id' => id }]
  end

  def evaluate(entry, body)
    method = ACCESSORS.fetch(body['accessor'].to_s) { raise ArgumentError, "unknown accessor #{body['accessor']}" }
    detail = entry.client.public_send(method, body['flagKey'].to_s, body['context'], body['fallback'])
    [200, { 'value' => detail.value, 'variationIndex' => detail.variation_index, 'reason' => detail.reason.wire }]
  end

  def status_json(client)
    status = client.status
    { 'etag' => status.etag,
      'lastSyncAt' => status.last_sync_at && (status.last_sync_at.to_f * 1000).round,
      'stream' => status.stream.to_s, 'lastError' => status.last_error&.to_s }
  end

  def client_route(entry, method, action, body)
    case "#{method} #{action}"
    when 'POST ready' then [200, { 'ready' => entry.client.ready }]
    when 'POST evaluate' then evaluate(entry, body)
    when 'GET status' then [200, status_json(entry.client)]
    when 'GET hook-events' then [200, { 'events' => entry.lock.synchronize { entry.hook_events.dup } }]
    when 'GET errors'
      messages = entry.lock.synchronize { entry.errors.dup }
      [200, { 'count' => messages.size, 'messages' => messages }]
    when 'POST close'
      entry.client.close
      [200, {}]
    else raise NotFound, 'no such route'
    end
  end

  def route(method, path, body)
    if method == 'GET' && path == '/'
      return [200,
              { 'name' => 'toggleslash-sdk (Ruby)',
                'capabilities' => ['integer-accessor'] }]
    end
    return create(body) if method == 'POST' && path == '/clients'

    match = %r{\A/clients/([^/]+)/([a-z-]+)\z}.match(path)
    entry = match && CLIENTS_LOCK.synchronize { CLIENTS[match[1]] }
    raise NotFound, 'no such client' if entry.nil?

    client_route(entry, method, match[2], body)
  end

  def read_request(socket)
    request_line = socket.gets("\r\n")&.chomp
    return nil if request_line.nil?

    method, path = request_line.split
    length = 0
    while (line = socket.gets("\r\n")) && line != "\r\n"
      length = line.split(':', 2)[1].to_i if line.downcase.start_with?('content-length:')
    end
    text = length.positive? ? socket.read(length) : ''
    [method, path, text.nil? || text.empty? ? {} : ToggleSlash::Json.parse(text)]
  end

  def answer(socket, status, payload)
    text = payload.is_a?(String) ? payload : ToggleSlash::Json.generate(payload)
    body = text.b
    socket.write("HTTP/1.1 #{status} #{status == 200 ? 'OK' : 'Other'}\r\nContent-Type: application/json\r\n" \
                 "Content-Length: #{body.bytesize}\r\nConnection: close\r\n\r\n")
    socket.write(body)
  end

  def serve(socket)
    method, path, body = read_request(socket)
    return if method.nil?

    status, payload = route(method, path, body)
    answer(socket, status, payload)
  rescue NotFound => e
    answer(socket, 404, e.message)
  rescue StandardError, ScriptError => e
    answer(socket, 500, "#{e.class}: #{e.message}\n#{e.backtrace&.first(8)&.join("\n")}")
  ensure
    socket.close
  end

  def run
    server = TCPServer.new('127.0.0.1', 0)
    $stdout.sync = true
    puts "listening http://127.0.0.1:#{server.addr[1]}"
    loop do
      socket = server.accept
      Thread.new(socket) do |connection|
        Thread.current.report_on_exception = false
        serve(connection)
      end
    end
  end
end

TestService.run if $PROGRAM_NAME == __FILE__
