# frozen_string_literal: true

require "strscan"

module Toggleslash
  # A JSON reader and writer that hold what the spec needs and the standard library does not: binary64
  # numbers (1e999 is an infinity, with no warning), lone surrogates kept in strings, a byte order mark
  # skipped, invalid UTF-8 replaced the WHATWG way, the last duplicate key winning.
  module Json
    class ParseError < StandardError; end

    MAX_DEPTH = 1000
    EXPONENT_LIMIT = 400
    NEGATIVE_ZERO = -0.0
    POSITIVE_ZERO = 0.0

    module_function

    # Parses `input` (a String of bytes). Raises ParseError for text that is not JSON.
    def parse(input)
      text = input.b
      text = text.byteslice(3, text.bytesize - 3) if text.start_with?("\xEF\xBB\xBF".b)
      text.force_encoding(Encoding::UTF_8)
      text = text.scrub("�") unless text.valid_encoding?
      Reader.new(text).read
    rescue SystemStackError
      raise ParseError, "nested too deeply"
    end

    # Writes a value as JSON text. Every code unit above U+007F is escaped, so a lone surrogate survives.
    def generate(value)
      out = +""
      write(value, out, 0)
      out
    end

    def write(value, out, depth)
      raise ParseError, "nested too deeply" if depth > MAX_DEPTH + 1

      case value
      when true then out << "true"
      when false then out << "false"
      when Integer then out << value.to_s
      when Float then write_float(value, out)
      when String then write_string(value, out)
      when Symbol then write_string(value.to_s, out)
      when Array then write_array(value, out, depth)
      when Hash then write_object(value, out, depth)
      else out << "null" # nil, and anything that is not a JSON value
      end
    end

    def write_float(value, out)
      out << if value.nan?
               "null"
             elsif value.infinite?
               (value.positive? ? "1e999" : "-1e999")
             else
               value.to_s
             end
    end

    def write_array(value, out, depth)
      out << "["
      index = 0
      count = value.size
      while index < count
        out << "," if index.positive?
        write(value[index], out, depth + 1)
        index += 1
      end
      out << "]"
    end

    def write_object(value, out, depth)
      out << "{"
      keys = value.keys
      index = 0
      while index < keys.size
        key = keys[index]
        out << "," if index.positive?
        write_string(key.to_s, out)
        out << ":"
        write(value[key], out, depth + 1)
        index += 1
      end
      out << "}"
    end

    ESCAPES = { '"' => '\\"', "\\" => "\\\\", "\n" => "\\n", "\r" => "\\r", "\t" => "\\t", "\b" => "\\b",
                "\f" => "\\f" }.freeze

    def write_string(value, out)
      out << '"'
      text = Text.utf8(value)
      if text.ascii_only?
        text.each_char { |ch| out << escape_ascii(ch) }
      else
        Text.units(text).each do |unit|
          out << (unit < 0x80 ? escape_ascii(unit.chr) : format("\\u%04x", unit))
        end
      end
      out << '"'
    end

    def escape_ascii(char)
      escaped = ESCAPES[char]
      return escaped if escaped
      return format("\\u%04x", char.ord) if char.ord < 0x20 || char.ord == 0x7F

      char
    end

    # Converts the text of a JSON number to a Ruby number: an Integer for a short integer literal, a Float
    # otherwise. Never warns, whatever the exponent.
    def number(negative, int, frac, exp)
      if frac.nil? && exp.nil? && int.length <= 15
        return -0.0 if negative && int == "0"

        value = int.to_i
        return negative ? -value : value
      end
      to_double(negative, int, frac || "", exp)
    end

    def to_double(negative, int, frac, exp)
      digits = int + frac
      significant = digits.sub(/\A0+/, "")
      return negative ? NEGATIVE_ZERO : POSITIVE_ZERO if significant.empty?

      exponent = exp ? exp.to_i : 0
      magnitude = int.length - (digits.length - significant.length) + exponent
      return negative ? -Float::INFINITY : Float::INFINITY if magnitude > EXPONENT_LIMIT
      return negative ? NEGATIVE_ZERO : POSITIVE_ZERO if magnitude < -EXPONENT_LIMIT

      scale = exponent - frac.length
      mantissa = digits.to_i
      rational = scale >= 0 ? Rational(mantissa * (10**scale)) : Rational(mantissa, 10**-scale)
      result = rational.to_f
      negative ? -result : result
    end

    # The reader: one pass over the text with an explicit nesting depth.
    class Reader
      SIMPLE = { '"' => '"', "\\" => "\\", "/" => "/", "b" => "\b", "f" => "\f", "n" => "\n", "r" => "\r",
                 "t" => "\t" }.freeze

      def initialize(text)
        @s = StringScanner.new(text)
        @depth = 0
      end

      def read
        skip_space
        value = read_value
        skip_space
        fail_parse("trailing data") unless @s.eos?
        value
      end

      private

      def fail_parse(message)
        raise ParseError, "#{message} at byte #{@s.pos}"
      end

      def skip_space
        @s.skip(/[ \t\n\r]*/)
      end

      def read_value
        skip_space
        case @s.peek(1)
        when "{" then read_object
        when "[" then read_array
        when '"' then read_string
        when "t" then literal("true", true)
        when "f" then literal("false", false)
        when "n" then literal("null", nil)
        when "-", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9" then read_number
        else fail_parse("unexpected character")
        end
      end

      def literal(word, value)
        fail_parse("bad literal") unless @s.scan(word)
        value
      end

      def enter
        @depth += 1
        fail_parse("nested too deeply") if @depth > MAX_DEPTH
      end

      def read_object
        enter
        @s.getch
        result = {}
        skip_space
        if @s.skip(/\}/)
          @depth -= 1
          return result
        end
        loop do
          skip_space
          fail_parse("expected a key") unless @s.peek(1) == '"'
          key = read_string
          skip_space
          fail_parse("expected a colon") unless @s.skip(/:/)
          result[key] = read_value
          skip_space
          next if @s.skip(/,/)
          break if @s.skip(/\}/)

          fail_parse("expected a comma or a closing brace")
        end
        @depth -= 1
        result
      end

      def read_array
        enter
        @s.getch
        result = []
        skip_space
        if @s.skip(/\]/)
          @depth -= 1
          return result
        end
        loop do
          result << read_value
          skip_space
          next if @s.skip(/,/)
          break if @s.skip(/\]/)

          fail_parse("expected a comma or a closing bracket")
        end
        @depth -= 1
        result
      end

      def read_number
        negative = !@s.skip(/-/).nil?
        int = @s.scan(/0|[1-9][0-9]*/)
        fail_parse("bad number") unless int
        frac = nil
        if @s.skip(/\./)
          frac = @s.scan(/[0-9]+/)
          fail_parse("bad number") unless frac
        end
        exp = nil
        if @s.scan(/[eE]/)
          sign = @s.scan(/[+-]/)
          digits = @s.scan(/[0-9]+/)
          fail_parse("bad number") unless digits
          exp = (sign == "-" ? "-" : "") + digits
        end
        Json.number(negative, int, frac, exp)
      end

      def read_string
        @s.getch
        buffer = nil
        loop do
          chunk = @s.scan(/[^"\\\x00-\x1f]+/)
          if chunk
            if buffer
              buffer << chunk.b
            else
              buffer = chunk.b
            end
          end
          buffer ||= String.new(encoding: Encoding::BINARY)
          char = @s.getch
          case char
          when '"'
            return buffer.force_encoding(Encoding::UTF_8)
          when "\\"
            buffer << escape.b
          else
            fail_parse("unterminated string or control character")
          end
        end
      end

      def escape
        char = @s.getch
        return SIMPLE[char] if SIMPLE.key?(char)

        fail_parse("bad escape") unless char == "u"

        unit = hex4
        if unit >= 0xD800 && unit <= 0xDBFF && @s.peek(2) == "\\u"
          save = @s.pos
          @s.pos += 2
          low = hex4_or_nil
          return [0x10000 + ((unit - 0xD800) << 10) + (low - 0xDC00)].pack("U") if low && low >= 0xDC00 && low <= 0xDFFF

          @s.pos = save
        end
        encode_unit(unit)
      end

      def hex4
        hex4_or_nil || fail_parse("bad unicode escape")
      end

      def hex4_or_nil
        digits = @s.scan(/[0-9a-fA-F]{4}/)
        digits&.to_i(16)
      end

      # A lone surrogate is written as its three-byte generalised UTF-8 form.
      def encode_unit(unit)
        return [unit].pack("U") unless unit >= 0xD800 && unit <= 0xDFFF

        [0xE0 | (unit >> 12), 0x80 | ((unit >> 6) & 0x3F), 0x80 | (unit & 0x3F)].pack("C*")
      end
    end
  end
end
