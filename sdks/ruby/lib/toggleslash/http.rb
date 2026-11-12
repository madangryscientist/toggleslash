# frozen_string_literal: true

require "socket"
require "openssl"
require "uri"

module Toggleslash
  # A small HTTP/1.1 client: one request per connection, `Connection: close`, no redirects, no proxy, no
  # compression. It is hand-written because the standard library's `Net::HTTP` bounds each socket operation,
  # not the whole request, and cannot be aborted from another thread, which contract 7.1, 7.2 and 11 need.
  # Every wait here is an `IO.select` against one deadline and the abort pipe, so a whole request (the body
  # included) is bounded, and `close` ends a request in flight by closing the pipe.
  module Http
    STREAM_IDLE = 60.0

    # The request ran past its bound, or the stream went quiet.
    class RequestTimeout < StandardError; end

    # The connection failed, or anything else went wrong with the request.
    class NetworkFailure < StandardError; end

    # `close` abandoned the request.
    class Aborted < StandardError; end

    Response = Struct.new(:status, :headers, :body) do
      # A header by lower-case name; the first one wins.
      def header(name)
        headers[name]
      end
    end

    def self.now
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    # The requests `close` may abandon. Closing the write end of a pipe makes its read end readable for every
    # request that waits on it, now and later.
    class Aborts
      def initialize
        @reader, @writer = IO.pipe
        @closed = false
        @lock = Mutex.new
      end

      attr_reader :reader

      def closed?
        @lock.synchronize { @closed }
      end

      def abort_all
        @lock.synchronize do
          return if @closed

          @closed = true
          @writer.close
        end
      end

      def dispose
        abort_all
        @lock.synchronize { @reader.close unless @reader.closed? }
      end
    end

    # A parsed base URL: scheme, host, port and a path prefix.
    class Endpoint
      attr_reader :host, :port, :prefix, :https

      def initialize(url)
        @valid = false
        @https = false
        @host = ""
        @port = 80
        @prefix = ""
        parse(url)
      end

      def valid?
        @valid
      end

      def host_header
        name = @host.include?(":") ? "[#{@host}]" : @host
        default = @https ? 443 : 80
        @port == default ? name : "#{name}:#{@port}"
      end

      private

      def parse(url)
        uri = URI.parse(url)
        return unless %w[http https].include?(uri.scheme) && uri.hostname && !uri.hostname.empty?

        @https = uri.scheme == "https"
        @host = uri.hostname
        @port = uri.port || (@https ? 443 : 80)
        return unless @port.between?(1, 65_535)

        @prefix = uri.path.to_s
        @valid = true
      rescue URI::Error, ArgumentError
        nil
      end
    end

    # One open connection, plain or TLS, with reads and writes bounded by a deadline and the abort pipe.
    class Conn
      def initialize(io, aborts)
        @io = io
        @aborts = aborts
      end

      def close
        @io.close unless @io.closed?
      rescue StandardError
        nil
      end

      # The next bytes, or nil at the end of the stream.
      def read_some(deadline)
        loop do
          chunk = @io.read_nonblock(65_536, exception: false)
          case chunk
          when :wait_readable then Conn.wait(@io, :read, deadline, @aborts)
          when :wait_writable then Conn.wait(@io, :write, deadline, @aborts)
          else return chunk
          end
        end
      rescue EOFError
        nil
      rescue SystemCallError, IOError, OpenSSL::SSL::SSLError => e
        raise Aborted if @aborts&.closed?

        raise NetworkFailure, e.class.name
      end

      def write_all(data, deadline)
        data = data.b
        until data.empty?
          written = @io.write_nonblock(data, exception: false)
          case written
          when :wait_readable then Conn.wait(@io, :read, deadline, @aborts)
          when :wait_writable then Conn.wait(@io, :write, deadline, @aborts)
          else data = data.byteslice(written, data.bytesize - written)
          end
        end
      rescue SystemCallError, IOError, OpenSSL::SSL::SSLError => e
        raise Aborted if @aborts&.closed?

        raise NetworkFailure, e.class.name
      end

      # Waits until `io` can be read or written, the deadline passes (RequestTimeout) or the abort pipe fires.
      def self.wait(io, mode, deadline, aborts)
        raise Aborted if aborts&.closed?

        remaining = deadline - Http.now
        raise RequestTimeout if remaining <= 0

        reads = mode == :read ? [io] : []
        writes = mode == :write ? [io] : []
        reads << aborts.reader if aborts
        ready = IO.select(reads, writes, nil, remaining)
        raise RequestTimeout unless ready
        raise Aborted if aborts && ready[0].include?(aborts.reader)
      rescue IOError, SystemCallError
        raise Aborted if aborts&.closed?

        raise NetworkFailure, "select"
      end
    end

    # The body of a response, read in pieces: by length, chunked, or until the server closes.
    class Body
      def initialize(conn, mode, length, buffer)
        @conn = conn
        @mode = mode
        @remaining = length
        @buffer = buffer
        @state = :size
        @chunk_left = 0
        @done = false
      end

      # The next bytes (at least one), or nil when the body has ended.
      def read(deadline)
        return nil if @done

        case @mode
        when :none then finish
        when :length then read_length(deadline)
        when :chunked then read_chunked(deadline)
        else read_until_close(deadline)
        end
      end

      private

      def finish
        @done = true
        nil
      end

      def fill(deadline)
        chunk = @conn.read_some(deadline)
        raise NetworkFailure, "truncated" if chunk.nil?

        @buffer << chunk
      end

      def take(limit)
        piece = @buffer.byteslice(0, limit)
        @buffer = @buffer.byteslice(piece.bytesize, @buffer.bytesize - piece.bytesize)
        piece
      end

      def read_length(deadline)
        return finish if @remaining.zero?

        fill(deadline) if @buffer.empty?
        piece = take(@remaining)
        @remaining -= piece.bytesize
        piece
      end

      def read_until_close(deadline)
        if @buffer.empty?
          chunk = @conn.read_some(deadline)
          return finish if chunk.nil?

          @buffer << chunk
        end
        take(@buffer.bytesize)
      end

      def line(deadline)
        loop do
          at = @buffer.index("\n".b)
          if at
            text = take(at + 1)
            return text.chomp.chomp("\r")
          end
          fill(deadline)
        end
      end

      def read_chunked(deadline)
        loop do
          case @state
          when :size then start_chunk(deadline)
          when :data
            fill(deadline) if @buffer.empty?
            piece = take(@chunk_left)
            @chunk_left -= piece.bytesize
            @state = :data_end if @chunk_left.zero?
            return piece
          when :data_end
            raise NetworkFailure, "bad chunk" unless line(deadline).empty?

            @state = :size
          else
            return finish if line(deadline).empty?
          end
        end
      end

      def start_chunk(deadline)
        text = line(deadline).split(";", 2).first.to_s.strip
        raise NetworkFailure, "bad chunk size" unless text.match?(/\A[0-9a-fA-F]{1,15}\z/)

        size = text.to_i(16)
        if size.zero?
          @state = :trailer
        else
          @chunk_left = size
          @state = :data
        end
      end
    end

    # An open response being read in pieces.
    class Stream
      attr_reader :status, :headers

      def initialize(conn, status, headers, body, bodyless)
        @conn = conn
        @status = status
        @headers = headers
        @body = body
        @bodyless = bodyless
        @idle = STREAM_IDLE
      end

      # True for an answer that declared `Content-Length: 0`: no body object at all.
      def bodyless?
        @bodyless
      end

      attr_writer :idle

      def header(name)
        @headers[name]
      end

      # The next bytes (at least one), or nil when the server ended the stream.
      def read
        @body.read(Http.now + @idle)
      end

      def close
        @conn.close
      end
    end

    # The default transport. A test replaces it with an object that has the same two methods.
    class Transport
      def initialize(ssl_context: nil)
        @ssl_context = ssl_context
      end

      # One whole request, body read, in `timeout` seconds. Raises RequestTimeout, Aborted or NetworkFailure
      # and nothing else.
      def fetch(endpoint, method, path, headers, body, timeout, aborts)
        guarded(aborts) do
          deadline = Http.now + timeout
          conn = open_connection(endpoint, deadline, aborts)
          begin
            conn.write_all(request(endpoint, method, path, headers, body), deadline)
            status, response_headers, reader, = read_response(conn, method, deadline)
            Response.new(status, response_headers, collect(reader, deadline))
          ensure
            conn.close
          end
        end
      end

      # Opens `GET path`: bounded by `open_timeout` until the headers arrive; after that every read waits at
      # most `idle_timeout` for a byte. The caller closes the stream.
      def open_stream(endpoint, path, headers, open_timeout, idle_timeout, aborts)
        guarded(aborts) do
          deadline = Http.now + open_timeout
          conn = open_connection(endpoint, deadline, aborts)
          begin
            conn.write_all(request(endpoint, "GET", path, headers, nil), deadline)
            status, response_headers, reader, bodyless = read_response(conn, "GET", deadline)
            stream = Stream.new(conn, status, response_headers, reader, bodyless)
            stream.idle = idle_timeout
            stream
          rescue Exception # rubocop:disable Lint/RescueException
            conn.close
            raise
          end
        end
      end

      private

      def guarded(aborts)
        yield
      rescue RequestTimeout, Aborted, NetworkFailure
        raise
      rescue StandardError, SystemStackError => e
        raise Aborted if aborts&.closed?

        raise NetworkFailure, e.class.name
      end

      def request(endpoint, method, path, headers, body)
        raise NetworkFailure, "unusable url" unless endpoint.valid?

        lines = ["#{method} #{endpoint.prefix}#{path} HTTP/1.1", "Host: #{endpoint.host_header}", "Connection: close"]
        lines << "Content-Length: #{body.bytesize}" if body
        headers.each do |name, value|
          raise NetworkFailure, "unusable header" if value.match?(/[\r\n]/)

          lines << "#{name}: #{value}"
        end
        head = "#{lines.join("\r\n")}\r\n\r\n".b
        body ? head + body.b : head
      end

      def open_connection(endpoint, deadline, aborts)
        raise NetworkFailure, "unusable url" unless endpoint.valid?
        raise Aborted if aborts&.closed?

        sock = connect(endpoint, deadline, aborts)
        io = endpoint.https ? tls(sock, endpoint, deadline, aborts) : sock
        Conn.new(io, aborts)
      end

      def connect(endpoint, deadline, aborts)
        failure = nil
        addresses(endpoint, deadline, aborts).each do |address|
          return connect_one(address, deadline, aborts)
        rescue NetworkFailure => e
          failure = e
        end
        raise failure || NetworkFailure.new("no address")
      end

      def connect_one(address, deadline, aborts)
        sock = Socket.new(address.afamily, Socket::SOCK_STREAM, 0)
        begin
          sock.setsockopt(Socket::IPPROTO_TCP, Socket::TCP_NODELAY, 1)
          begin
            sock.connect_nonblock(address)
          rescue IO::WaitWritable
            Conn.wait(sock, :write, deadline, aborts)
            begin
              sock.connect_nonblock(address)
            rescue Errno::EISCONN
              nil
            end
          end
          sock
        rescue SystemCallError, SocketError, IOError => e
          sock.close
          raise NetworkFailure, e.class.name
        rescue Exception # rubocop:disable Lint/RescueException
          sock.close
          raise
        end
      end

      # The addresses of the host. A numeric host never blocks; a name is resolved on a helper thread so the
      # wait stays bounded and abortable.
      def addresses(endpoint, deadline, aborts)
        host = endpoint.host
        begin
          return [Addrinfo.tcp(host, endpoint.port)] if numeric?(host)
        rescue SocketError
          nil
        end
        queue = Queue.new
        resolver = Thread.new do
          Thread.current.report_on_exception = false
          queue << Addrinfo.getaddrinfo(host, endpoint.port, nil, :STREAM)
        rescue StandardError => e
          queue << e
        end
        result = pop(queue, deadline, aborts)
        resolver.kill if result.nil?
        raise NetworkFailure, "resolve" if result.is_a?(StandardError) || result.empty?

        result
      end

      def numeric?(host)
        host.match?(/\A[0-9.]+\z/) || host.include?(":")
      end

      def pop(queue, deadline, aborts)
        loop do
          raise Aborted if aborts&.closed?

          remaining = deadline - Http.now
          raise RequestTimeout if remaining <= 0

          value = queue.pop(timeout: [remaining, 0.05].min)
          return value unless value.nil?
        end
      end

      def tls(sock, endpoint, deadline, aborts)
        context = @ssl_context || default_context
        ssl = OpenSSL::SSL::SSLSocket.new(sock, context)
        ssl.hostname = endpoint.host
        ssl.sync_close = true
        begin
          loop do
            result = ssl.connect_nonblock(exception: false)
            case result
            when :wait_readable then Conn.wait(sock, :read, deadline, aborts)
            when :wait_writable then Conn.wait(sock, :write, deadline, aborts)
            else break
            end
          end
          # A context the caller supplied may not check the name; the check is never skipped.
          ssl.post_connection_check(endpoint.host) unless context.verify_mode == OpenSSL::SSL::VERIFY_NONE
        rescue OpenSSL::SSL::SSLError, SystemCallError, IOError => e
          ssl.close
          raise NetworkFailure, e.class.name
        rescue Exception # rubocop:disable Lint/RescueException
          ssl.close
          raise
        end
        ssl
      end

      def default_context
        context = OpenSSL::SSL::SSLContext.new
        context.set_params(verify_mode: OpenSSL::SSL::VERIFY_PEER, verify_hostname: true)
        context
      end

      # [status, headers, Body, bodyless] once the head is in. Interim 1xx answers are skipped.
      def read_response(conn, method, deadline)
        buffer = String.new(encoding: Encoding::BINARY)
        loop do
          head, buffer = read_head(conn, buffer, deadline)
          status, headers = parse_head(head)
          next if status >= 100 && status < 200 && status != 101

          mode, length = framing(status, method, headers)
          return [status, headers, Body.new(conn, mode, length, buffer), mode == :length && length.zero?]
        end
      end

      def read_head(conn, buffer, deadline)
        loop do
          at = buffer.index("\r\n\r\n".b)
          return [buffer.byteslice(0, at), buffer.byteslice(at + 4, buffer.bytesize - at - 4)] if at

          chunk = conn.read_some(deadline)
          raise NetworkFailure, "no response" if chunk.nil?

          buffer << chunk
        end
      end

      def parse_head(head)
        lines = head.split("\r\n", -1)
        match = lines.shift.to_s.match(%r{\AHTTP/1\.[01] ([0-9]{3})(?: |\z)})
        raise NetworkFailure, "bad status line" unless match

        headers = {}
        lines.each do |line|
          name, colon, value = line.partition(":")
          next if colon.empty?

          key = name.strip.downcase
          headers[key] ||= value.strip.force_encoding(Encoding::UTF_8).scrub("�")
        end
        [match[1].to_i, headers]
      end

      def framing(status, method, headers)
        return [:none, 0] if method == "HEAD" || status == 204 || status == 304 || status == 101

        coding = headers["transfer-encoding"]
        return [:chunked, 0] if coding && coding.downcase.split(",").last.to_s.strip == "chunked"

        length = headers["content-length"]
        return [:close, 0] unless length
        raise NetworkFailure, "bad content-length" unless length.match?(/\A[0-9]{1,15}\z/)

        [:length, length.to_i]
      end

      def collect(reader, deadline)
        out = String.new(encoding: Encoding::BINARY)
        while (piece = reader.read(deadline))
          out << piece
        end
        out
      end
    end
  end
end
