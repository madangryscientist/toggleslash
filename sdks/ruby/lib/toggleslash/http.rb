# frozen_string_literal: true

require 'ipaddr'
require 'openssl'
require 'socket'
require 'uri'
require_relative 'clock'

module ToggleSlash
  # A small HTTP/1.1 client, written because `Net::HTTP` bounds each read and not the whole request: here one
  # monotonic deadline covers the connect, the TLS handshake, the request, the headers and the body, and a
  # `Cancel` abandons a request that is blocked on the network. One request per connection (`Connection:
  # close`), no pool, no redirects, no compression.
  module Http
    class Error < StandardError; end

    # The deadline passed before the answer was complete.
    class Timeout < Error; end

    # The connection failed, was cut, or spoke something that is not HTTP/1.1.
    class Network < Error; end

    # The caller's `Cancel` was thrown while the request was waiting.
    class Cancelled < Error; end

    # The longest a server's `Retry-After` can hold the client, in seconds.
    MAX_RETRY_AFTER = 300
    MAX_HEAD_BYTES = 65_536

    # Where requests go: `prefix` is whatever path the configured URL carries, `host_header` the Host value.
    Target = Struct.new(:scheme, :host, :port, :prefix, :host_header)

    module_function

    # Splits the configured URL. Nil when it is not an http or https URL with a host; a request to nil
    # fails with `Network`, so a bad URL is a `network` error and never an exception at the caller.
    def target(base_url)
      uri = URI.parse(base_url)
      return nil unless %w[http https].include?(uri.scheme) && uri.hostname && !uri.hostname.empty?

      port = uri.port || (uri.scheme == 'https' ? 443 : 80)
      default = uri.scheme == 'https' ? 443 : 80
      host = uri.hostname.include?(':') ? "[#{uri.hostname}]" : uri.hostname
      Target.new(uri.scheme, uri.hostname, port, uri.path.to_s, port == default ? host : "#{host}:#{port}")
    rescue URI::Error
      nil
    end

    # The seconds a response asks the client to wait, capped, or nil. Only a 429 counts, and only whole
    # decimal seconds (surrounding spaces ignored): a date, a fraction or a sign is ignored.
    def retry_after(status, header)
      return nil unless status == 429

      text = header.to_s.strip
      return nil unless text.match?(/\A[0-9]+\z/)

      [text.to_i, MAX_RETRY_AFTER].min
    end

    # Sends one request and returns once the response head has arrived; the body is read afterwards, by the
    # same deadline. Raises `Timeout`, `Network` or `Cancelled`, never anything else.
    def request(target, method, path, deadline:, cancel: nil, headers: {}, body: nil)
      raise Network, 'the url is not an http or https url' if target.nil?

      connection = Connection.connect(target, deadline, cancel)
      begin
        connection.write_all(build_head(target, method, path, headers, body), deadline)
        Response.read_head(connection, deadline, method)
      rescue Exception # rubocop:disable Lint/RescueException
        connection.close
        raise
      end
    rescue Error
      raise
    rescue SystemCallError, IOError, SocketError, OpenSSL::SSL::SSLError => e
      raise Network, "network failure (#{e.class})"
    end

    def build_head(target, method, path, headers, body)
      lines = ["#{method} #{target.prefix}#{path} HTTP/1.1", "Host: #{target.host_header}", 'Connection: close']
      headers.each do |name, value|
        raise Network, 'a header value holds a line break' if value.to_s.match?(/[\r\n]/)

        lines << "#{name}: #{value}"
      end
      lines << "Content-Length: #{body.bytesize}" unless body.nil?
      head = "#{lines.join("\r\n")}\r\n\r\n".b
      body.nil? ? head : head << body.b
    end

    # A connected socket, plain or TLS, and the waits on it.
    class Connection
      def self.connect(target, deadline, cancel)
        addresses = resolve(target.host, target.port, deadline, cancel)
        failure = nil
        addresses.each do |address|
          socket = open_socket(address, deadline, cancel)
          return target.scheme == 'https' ? start_tls(socket, target.host, deadline,
                                                      cancel) : new(socket, socket, cancel)
        rescue SystemCallError => e
          failure = e
        end
        raise Network, "connection failed (#{failure ? failure.class : 'no address'})"
      end

      def self.ip_literal?(host)
        IPAddr.new(host)
        true
      rescue IPAddr::Error
        false
      end

      # Name resolution cannot be bounded by a socket call, so it runs on a helper thread that is waited on
      # in short slices against the deadline and the cancel switch.
      def self.resolve(host, port, deadline, cancel)
        return [Addrinfo.tcp(host, port)] if ip_literal?(host)

        lookup = Thread.new do
          Thread.current.report_on_exception = false
          Addrinfo.getaddrinfo(host, port, nil, :STREAM)
        end
        until lookup.join([deadline - Clock.monotonic, 0.05].min.clamp(0.0, 0.05))
          if cancel&.cancelled?
            lookup.kill
            raise Cancelled, 'cancelled'
          end
          next if Clock.monotonic < deadline

          lookup.kill
          raise Timeout, 'timed out resolving the host'
        end
        lookup.value
      end

      def self.open_socket(address, deadline, cancel)
        socket = Socket.new(address.afamily, Socket::SOCK_STREAM, 0)
        begin
          socket.setsockopt(Socket::IPPROTO_TCP, Socket::TCP_NODELAY, 1)
          if socket.connect_nonblock(address, exception: false) == :wait_writable
            new(socket, socket, cancel).wait(:write, deadline)
            code = socket.getsockopt(Socket::SOL_SOCKET, Socket::SO_ERROR).int
            raise SystemCallError.new('connect', code) unless code.zero?
          end
          socket
        rescue Exception # rubocop:disable Lint/RescueException
          socket.close
          raise
        end
      end

      def self.tls_context
        @tls_context ||= OpenSSL::SSL::SSLContext.new.tap(&:set_params)
      end

      def self.start_tls(socket, host, deadline, cancel)
        ssl = OpenSSL::SSL::SSLSocket.new(socket, tls_context)
        ssl.sync_close = true
        ssl.hostname = host unless ip_literal?(host)
        connection = new(socket, ssl, cancel)
        begin
          loop do
            result = ssl.connect_nonblock(exception: false)
            break if result == ssl

            connection.wait(result == :wait_writable ? :write : :read, deadline)
          end
          ssl.post_connection_check(host)
          connection
        rescue Exception # rubocop:disable Lint/RescueException
          connection.close
          raise
        end
      end

      def initialize(socket, io, cancel)
        @socket = socket
        @io = io
        @cancel = cancel
      end

      # Blocks until the socket can be read or written, the deadline passes, or the cancel switch is thrown.
      def wait(mode, deadline)
        raise Cancelled, 'cancelled' if @cancel&.cancelled?

        remaining = deadline - Clock.monotonic
        raise Timeout, 'timed out' unless remaining.positive?

        wake = @cancel&.reader
        readers = mode == :read ? [@socket] : []
        readers << wake if wake
        ready = IO.select(readers, mode == :write ? [@socket] : [], nil, remaining)
        raise Timeout, 'timed out' if ready.nil?
        raise Cancelled, 'cancelled' if wake && ready[0].include?(wake)
      end

      # The next bytes, or nil at the end of the stream.
      def read_some(deadline)
        loop do
          data = @io.read_nonblock(16_384, exception: false)
          case data
          when :wait_readable then wait(:read, deadline)
          when :wait_writable then wait(:write, deadline)
          else return data
          end
        end
      end

      def write_all(data, deadline)
        data = data.b
        until data.empty?
          written = @io.write_nonblock(data, exception: false)
          case written
          when :wait_writable then wait(:write, deadline)
          when :wait_readable then wait(:read, deadline)
          else data = data.byteslice(written..)
          end
        end
      end

      def close
        @io.close unless @io.closed?
        @socket.close unless @socket.closed?
      rescue IOError, SystemCallError, OpenSSL::SSL::SSLError
        nil
      end
    end

    # A response whose head has been read. The body is read on demand, whole or piece by piece.
    class Response
      attr_reader :status, :headers

      def self.read_head(connection, deadline, method)
        buffer = ''.b
        until (at = buffer.index("\r\n\r\n"))
          raise Network, 'the response head is too long' if buffer.bytesize > MAX_HEAD_BYTES

          data = connection.read_some(deadline)
          raise Network, 'the connection closed before the response head' if data.nil?

          buffer << data
        end
        head = buffer.byteslice(0, at)
        new(connection, head, buffer.byteslice((at + 4)..), method)
      end

      def initialize(connection, head, leftover, method)
        @connection = connection
        @buffer = leftover
        status_line, *lines = head.split("\r\n")
        @status = parse_status(status_line)
        @headers = parse_headers(lines)
        @done = false
        choose_framing(method)
      end

      def header(name)
        @headers[name.downcase]
      end

      # True when the response has no body at all (so a 200 with none is an error for a stream).
      def bodiless?
        @mode == :none || (@mode == :length && @remaining.zero?)
      end

      # The rest of the body, read under `deadline`.
      def read_body(deadline)
        body = ''.b
        while (piece = read_chunk(deadline: deadline))
          body << piece
        end
        body
      end

      # The next piece of the decoded body, or nil at its end. Each read waits at most until `deadline`, or
      # for `idle` seconds from the start of that read when `idle` is given, so any bytes restart the wait.
      def read_chunk(deadline: nil, idle: nil)
        loop do
          piece = take
          return piece unless piece.empty?
          return nil if @done

          data = @connection.read_some(idle ? Clock.monotonic + idle : deadline)
          if data.nil?
            raise Network, 'the connection closed before the body ended' unless @mode == :eof

            @done = true
            return nil
          end
          @buffer << data
        end
      end

      def close
        @connection.close
      end

      private

      def parse_status(line)
        match = %r{\AHTTP/1\.[01] ([0-9]{3})(?: |\z)}n.match(line.to_s)
        raise Network, 'the answer is not HTTP/1.x' if match.nil?

        match[1].to_i
      end

      def parse_headers(lines)
        lines.each_with_object({}) do |line, result|
          name, value = line.split(':', 2)
          next if value.nil?

          key = name.strip.downcase
          text = value.strip.force_encoding(Encoding::UTF_8)
          result[key] = result.key?(key) ? "#{result[key]}, #{text}" : text
        end
      end

      def choose_framing(method)
        length = @headers['content-length']
        if method == 'HEAD' || @status < 200 || [204, 304].include?(@status)
          @mode = :none
          @done = true
        elsif @headers['transfer-encoding'].to_s.downcase.include?('chunked')
          @mode = :chunked
          @chunk_state = :size
          @chunk_size = 0
        elsif length&.match?(/\A[0-9]+\z/)
          @mode = :length
          @remaining = length.to_i
          @done = @remaining.zero?
        else
          @mode = :eof
        end
      end

      # Decodes what is buffered; an empty string means more bytes are needed (or the body is done).
      def take
        case @mode
        when :none then ''.b
        when :eof then drain
        when :length then take_length
        else take_chunked
        end
      end

      def drain
        data = @buffer
        @buffer = ''.b
        data
      end

      def take_length
        count = [@buffer.bytesize, @remaining].min
        data = @buffer.byteslice(0, count)
        @buffer = @buffer.byteslice(count..)
        @remaining -= count
        @done = true if @remaining.zero?
        data
      end

      def take_chunked
        out = ''.b
        until @done
          progress = case @chunk_state
                     when :size then chunk_size_line
                     when :data then chunk_data(out)
                     when :data_end then chunk_data_end
                     else chunk_trailer
                     end
          break unless progress
        end
        out
      end

      def line_from_buffer
        at = @buffer.index("\r\n")
        if at.nil?
          raise Network, 'a chunk header is too long' if @buffer.bytesize > 4096

          return nil
        end
        line = @buffer.byteslice(0, at)
        @buffer = @buffer.byteslice((at + 2)..)
        line
      end

      def chunk_size_line
        line = line_from_buffer
        return false if line.nil?

        size = line.split(';', 2).first.to_s.strip
        raise Network, 'a chunk size is not hexadecimal' unless size.match?(/\A[0-9a-fA-F]+\z/)

        @chunk_size = size.to_i(16)
        @chunk_state = @chunk_size.zero? ? :trailer : :data
        true
      end

      def chunk_data(out)
        return false if @buffer.empty?

        count = [@buffer.bytesize, @chunk_size].min
        out << @buffer.byteslice(0, count)
        @buffer = @buffer.byteslice(count..)
        @chunk_size -= count
        @chunk_state = :data_end if @chunk_size.zero?
        true
      end

      def chunk_data_end
        return false if @buffer.bytesize < 2
        raise Network, 'a chunk is not followed by a line break' unless @buffer.start_with?("\r\n")

        @buffer = @buffer.byteslice(2..)
        @chunk_state = :size
        true
      end

      def chunk_trailer
        line = line_from_buffer
        return false if line.nil?

        @done = true if line.empty?
        true
      end
    end
  end
end
