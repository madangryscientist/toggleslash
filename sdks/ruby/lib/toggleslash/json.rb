# frozen_string_literal: true

require 'strscan'
require_relative 'eval/text'

module ToggleSlash
  # A JSON parser and generator that keep what docs/evaluation.md ("Reading JSON") asks for and the standard
  # `json` library does not: numbers as IEEE-754 binary64 (`1e999` is Infinity, `9007199254740993` reads as
  # `9007199254740992`), `-0.0`, a lone surrogate (`"\ud800"`) kept as its own three bytes, and the last of
  # two duplicate keys winning.
  #
  # A string is a Ruby String in UTF-8 (WTF-8 when it holds a lone surrogate, so it is then not
  # `valid_encoding?`). An object is a Hash with String keys, an array an Array, `null` is nil.
  module Json
    class ParseError < StandardError; end

    MAX_DEPTH = 512
    # An integer literal of at most this many digits is exact in binary64, so it stays an Integer.
    SAFE_DIGITS = 15
    WHITESPACE = /[ \t\n\r]*/
    NUMBER = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/
    PLAIN = /[^"\\\x00-\x1f]+/n
    ESCAPES = { '"' => '"', '\\' => '\\', '/' => '/', 'b' => "\b", 'f' => "\f", 'n' => "\n", 'r' => "\r",
                't' => "\t" }.freeze

    # One parse.
    class Parser
      # The scan runs over bytes, so a text that is not valid UTF-8 cannot make a regexp raise.
      def initialize(text)
        @scanner = StringScanner.new(text.b)
      end

      def parse
        skip_space
        value = read_value(0)
        skip_space
        fail_at('unexpected text after the value') unless @scanner.eos?
        value
      end

      private

      def fail_at(message)
        raise ParseError, "#{message} at byte #{@scanner.pos}"
      end

      def skip_space
        @scanner.skip(WHITESPACE)
      end

      def read_value(depth)
        fail_at('nested too deeply') if depth > MAX_DEPTH
        case @scanner.peek(1)
        when '{' then read_object(depth)
        when '[' then read_array(depth)
        when '"' then read_string
        when 't' then literal('true', true)
        when 'f' then literal('false', false)
        when 'n' then literal('null', nil)
        else read_number
        end
      end

      def literal(word, value)
        fail_at("expected #{word}") unless @scanner.skip(/#{word}/)
        value
      end

      def read_object(depth)
        @scanner.getch
        result = {}
        skip_space
        return result if @scanner.skip(/\}/)

        loop do
          skip_space
          fail_at('expected a string key') unless @scanner.peek(1) == '"'
          key = read_string
          skip_space
          fail_at('expected :') unless @scanner.skip(/:/)
          skip_space
          result[key] = read_value(depth + 1)
          skip_space
          next if @scanner.skip(/,/)
          return result if @scanner.skip(/\}/)

          fail_at('expected , or }')
        end
      end

      def read_array(depth)
        @scanner.getch
        result = []
        skip_space
        return result if @scanner.skip(/\]/)

        loop do
          skip_space
          result << read_value(depth + 1)
          skip_space
          next if @scanner.skip(/,/)
          return result if @scanner.skip(/\]/)

          fail_at('expected , or ]')
        end
      end

      def read_number
        text = @scanner.scan(NUMBER)
        fail_at('unexpected character') if text.nil?
        Json.number(text)
      end

      def read_string
        @scanner.getch
        buffer = String.new(encoding: Encoding::BINARY)
        loop do
          chunk = @scanner.scan(PLAIN)
          buffer << chunk.b if chunk
          char = @scanner.getch
          fail_at('unterminated string') if char.nil?
          return buffer.force_encoding(Encoding::UTF_8) if char == '"'

          fail_at('control character in a string') unless char == '\\'

          read_escape(buffer)
        end
      end

      def read_escape(buffer)
        char = @scanner.getch
        fail_at('unterminated escape') if char.nil?
        if char == 'u'
          append_unit(buffer, read_hex4)
        elsif ESCAPES.key?(char)
          buffer << ESCAPES[char].b
        else
          fail_at('bad escape')
        end
      end

      def read_hex4
        hex = @scanner.scan(/[0-9a-fA-F]{4}/)
        fail_at('bad \\u escape') if hex.nil?
        hex.to_i(16)
      end

      # A high surrogate followed by an escaped low one is one code point; any other surrogate stays alone.
      def append_unit(buffer, unit)
        if unit.between?(0xD800, 0xDBFF) && @scanner.check(/\\u[dD][c-fC-F][0-9a-fA-F]{2}/)
          @scanner.skip(/\\u/)
          low = read_hex4
          return buffer << [0x10000 + ((unit - 0xD800) << 10) + (low - 0xDC00)].pack('U').b
        end
        buffer << if unit.between?(0xD800, 0xDFFF)
                    [0xE0 | (unit >> 12), 0x80 | ((unit >> 6) & 0x3F), 0x80 | (unit & 0x3F)].pack('C*')
                  else
                    [unit].pack('U').b
                  end
      end
    end

    module_function

    # Parses a JSON text. Raises ParseError when it is not one.
    def parse(text)
      Parser.new(text).parse
    end

    # A number literal as Integer (exact, at most 15 digits) or Float (binary64).
    def number(text)
      return text.to_i if text.match?(/\A-?[0-9]{1,#{SAFE_DIGITS}}\z/) && text != '-0'

      float(text)
    end

    # `Float(text)` without the "out of range" warning: a literal far outside binary64 is Infinity or zero.
    def float(text)
      negative = text.start_with?('-')
      mantissa, exponent = text.delete_prefix('-').split(/[eE]/, 2)
      integer, fraction = mantissa.split('.', 2)
      digits = integer.sub(/\A0+/, '')
      scale = exponent.to_i
      if digits.empty?
        leading = (fraction || '').length - (fraction || '').sub(/\A0+/, '').length
        return signed(0.0, negative) if (fraction || '').delete('0').empty?

        scale -= leading
      else
        scale += digits.length
      end
      return signed(Float::INFINITY, negative) if scale > 310
      return signed(0.0, negative) if scale < -325

      Float(text)
    end

    def signed(value, negative)
      negative ? -value : value
    end

    # Freezes a parsed value and everything in it, so a value handed to a caller cannot change a ruleset.
    def freeze_deep(value)
      case value
      when Hash then value.each_value { |member| freeze_deep(member) }
      when Array then value.each { |member| freeze_deep(member) }
      end
      value.freeze
    end

    # Generates a JSON text. A Hash key may be a String or a Symbol. A lone surrogate is written as `\ud800`,
    # Infinity as `1e999` (which parses back to it) and NaN as null.
    def generate(value)
      out = String.new(encoding: Encoding::UTF_8)
      write(out, value)
      out
    end

    def write(out, value)
      case value
      when nil then out << 'null'
      when true then out << 'true'
      when false then out << 'false'
      when Integer then out << value.to_s
      when Float then out << float_text(value)
      when String then write_string(out, value)
      when Symbol then write_string(out, value.to_s)
      when Array then write_array(out, value)
      when Hash then write_object(out, value)
      else write_string(out, value.to_s)
      end
    end

    def float_text(value)
      return 'null' if value.nan?
      return (value.positive? ? '1e999' : '-1e999') if value.infinite?
      return '-0.0' if value.zero? && (1.0 / value).negative?

      value.to_s
    end

    def write_array(out, array)
      out << '['
      array.each_with_index do |item, index|
        out << ',' if index.positive?
        write(out, item)
      end
      out << ']'
    end

    def write_object(out, hash)
      out << '{'
      first = true
      hash.each do |key, item|
        out << ',' unless first
        first = false
        write_string(out, key.to_s)
        out << ':'
        write(out, item)
      end
      out << '}'
    end

    SHORT = { 0x22 => '\\"', 0x5C => '\\\\', 0x08 => '\\b', 0x0C => '\\f', 0x0A => '\\n', 0x0D => '\\r',
              0x09 => '\\t' }.freeze

    def write_string(out, string)
      out << '"'
      Eval::Text.codepoints(string).each do |point|
        out << if SHORT.key?(point) then SHORT[point]
               elsif point < 0x20 then format('\\u%04x', point)
               elsif point.between?(0xD800, 0xDFFF) then format('\\u%04x', point)
               else [point].pack('U')
               end
      end
      out << '"'
    end
  end
end
