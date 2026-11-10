# frozen_string_literal: true

module ToggleSlash
  # Server-Sent Events, read the way the WHATWG standard reads them. Fed bytes in any split, it returns the
  # events that are complete. A line ends at LF, CRLF or a lone CR (a CR at the end of a read waits for the
  # next byte, which may be its LF); a line starting with `:` is a comment; several `data` lines join with
  # LF; an event with no `data` line is dropped; the name is `message` when none was given; an event still
  # unfinished when the stream ends is never returned.
  module Sse
    Event = Struct.new(:name, :data)

    BOM = "\xEF\xBB\xBF".b.freeze

    class Parser
      def initialize
        @buffer = ''.b
        @started = false
        @name = nil
        @data = nil
      end

      # Bytes in, the events they complete out.
      def feed(bytes)
        @buffer << bytes.b
        skip_byte_order_mark
        events = []
        while (line = next_line)
          event = process(line)
          events << event if event
        end
        events
      end

      private

      def skip_byte_order_mark
        return if @started
        return if @buffer.bytesize < BOM.bytesize && BOM.start_with?(@buffer)

        @started = true
        @buffer = @buffer.byteslice(BOM.bytesize..) if @buffer.start_with?(BOM)
      end

      # The next complete line without its ending, or nil when the buffer holds none yet.
      def next_line
        return nil unless @started

        at = @buffer.index(/[\r\n]/n)
        return nil if at.nil?

        # A CR in the last position may be the first half of a CRLF: wait.
        return nil if @buffer.getbyte(at) == 0x0D && at == @buffer.bytesize - 1

        line = @buffer.byteslice(0, at)
        ending = @buffer.byteslice(at, 2) == "\r\n" ? 2 : 1
        @buffer = @buffer.byteslice((at + ending)..)
        line
      end

      def process(line)
        return dispatch if line.empty?
        return nil if line.getbyte(0) == 0x3A

        field, value = line.split(':', 2)
        value = value.nil? ? '' : value.delete_prefix(' ')
        case field
        when 'event' then @name = value
        when 'data' then @data = @data.nil? ? value : "#{@data}\n#{value}".b
        end
        nil
      end

      def dispatch
        data = @data
        name = @name
        @data = nil
        @name = nil
        return nil if data.nil?

        Event.new(text(name || 'message'), text(data))
      end

      def text(bytes)
        bytes.dup.force_encoding(Encoding::UTF_8).scrub
      end
    end
  end
end
