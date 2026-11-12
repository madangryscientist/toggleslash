# frozen_string_literal: true

module Toggleslash
  # A Server-Sent Events reader to the WHATWG rules the contract names (7.2, "Reading events").
  #
  # Feed it bytes as they arrive; it returns the events that were completed, as `[name, data]` pairs. A line
  # ends at LF, CRLF or a lone CR. Those bytes never occur inside a multi-byte UTF-8 character, so lines are
  # cut on bytes and each whole line is decoded alone (an invalid byte becomes U+FFFD as the WHATWG decoder
  # does it), which makes a character or a line ending split across reads harmless. A leading byte order mark
  # is dropped. A line that starts with `:` is a comment; only `event` and `data` are read; an event with no
  # `data` line is dropped; an event still incomplete when the stream ends is discarded (the caller just
  # stops feeding).
  class SseParser
    BOM = "\xEF\xBB\xBF".b.freeze

    def initialize
      @buffer = String.new(encoding: Encoding::BINARY)
      @skip_lf = false
      @bom_checked = false
      @name = +""
      @data = []
    end

    def feed(chunk)
      @buffer << chunk.b
      return [] unless strip_bom

      if @skip_lf && !@buffer.empty?
        @buffer = @buffer.byteslice(1, @buffer.bytesize - 1) if @buffer.getbyte(0) == 10
        @skip_lf = false
      end
      cut_lines
    end

    private

    # False while the first bytes could still turn out to be a byte order mark.
    def strip_bom
      return true if @bom_checked

      size = [@buffer.bytesize, BOM.bytesize].min
      return false if size < BOM.bytesize && @buffer.byteslice(0, size) == BOM.byteslice(0, size)

      @buffer = @buffer.byteslice(BOM.bytesize, @buffer.bytesize - BOM.bytesize) if @buffer.start_with?(BOM)
      @bom_checked = true
    end

    def cut_lines
      events = []
      start = 0
      index = 0
      size = @buffer.bytesize
      while index < size
        byte = @buffer.getbyte(index)
        if [10, 13].include?(byte)
          event = line(@buffer.byteslice(start, index - start))
          events << event if event
          if byte == 13
            if index + 1 < size
              index += 1 if @buffer.getbyte(index + 1) == 10
            else
              @skip_lf = true
            end
          end
          start = index + 1
        end
        index += 1
      end
      @buffer = @buffer.byteslice(start, size - start)
      events
    end

    def line(raw)
      return dispatch if raw.empty?
      return nil if raw.getbyte(0) == 58 # ":"

      text = raw.force_encoding(Encoding::UTF_8).scrub("�")
      name, colon, value = text.partition(":")
      value = value.delete_prefix(" ") unless colon.empty?
      case name
      when "event" then @name = value
      when "data" then @data << value
      end
      nil
    end

    def dispatch
      data = @data
      name = @name
      @data = []
      @name = +""
      return nil if data.empty?

      [name.empty? ? "message" : name, data.join("\n")]
    end
  end
end
