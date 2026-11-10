# frozen_string_literal: true

module ToggleSlash
  module Eval
    # Strings, as the evaluation spec reads them. A Ruby String holds WTF-8: UTF-8, plus a lone surrogate
    # kept as its own three bytes (so the string is not `valid_encoding?`, but it is byte-exact). These
    # helpers give the three views the spec needs: code points, UTF-16 code units (for order and the
    # ordinal string ops) and the bytes that are hashed (a lone surrogate becomes U+FFFD).
    module Text
      UTF8 = Encoding::UTF_8
      REPLACEMENT = 0xFFFD
      SURROGATES = (0xD800..0xDFFF)

      module_function

      # The string as UTF-8, whatever encoding it was given in.
      def utf8(string)
        encoding = string.encoding
        return string if encoding == UTF8
        return string.dup.force_encoding(UTF8) if [Encoding::BINARY, Encoding::US_ASCII].include?(encoding)

        string.encode(UTF8, invalid: :replace, undef: :replace)
      end

      # The code points: a surrogate pair is one, a lone surrogate is itself.
      def codepoints(string)
        text = utf8(string)
        text.valid_encoding? ? text.codepoints : decode(text.b)
      end

      # The UTF-16 code units, big-endian, as a binary string. Comparing two of these byte by byte is
      # comparing the strings by UTF-16 code unit, which is the order the spec asks for.
      def utf16be(string)
        text = utf8(string)
        return text.encode(Encoding::UTF_16BE).b if text.valid_encoding?

        units = []
        decode(text.b).each do |point|
          if point > 0xFFFF
            offset = point - 0x10000
            units << (0xD800 | (offset >> 10)) << (0xDC00 | (offset & 0x3FF))
          else
            units << point
          end
        end
        units.pack('n*')
      end

      # The bytes that are hashed: UTF-8, with each lone surrogate replaced by U+FFFD.
      def hash_bytes(string)
        text = utf8(string)
        return text.b if text.valid_encoding?

        decode(text.b).map { |point| SURROGATES.cover?(point) ? REPLACEMENT : point }.pack('U*').b
      end

      # Order by UTF-16 code unit. The ASCII case is the common one and needs no conversion.
      def compare(left, right)
        return left <=> right if left.ascii_only? && right.ascii_only?

        utf16be(left) <=> utf16be(right)
      end

      def include?(haystack, needle)
        return haystack.include?(needle) if haystack.ascii_only? && needle.ascii_only?

        long = utf16be(haystack)
        short = utf16be(needle)
        at = 0
        while (found = long.index(short, at))
          return true if found.even?

          at = found + 1
        end
        false
      end

      def start_with?(string, prefix)
        return string.start_with?(prefix) if string.ascii_only? && prefix.ascii_only?

        utf16be(string).start_with?(utf16be(prefix))
      end

      def end_with?(string, suffix)
        return string.end_with?(suffix) if string.ascii_only? && suffix.ascii_only?

        utf16be(string).end_with?(utf16be(suffix))
      end

      # A tolerant WTF-8 decoder for the strings that are not valid UTF-8. A byte that starts nothing
      # valid is U+FFFD; a surrogate is allowed; a high surrogate followed by a low one is one code point.
      def decode(bytes)
        points = []
        index = 0
        size = bytes.bytesize
        while index < size
          point, used = decode_one(bytes, index, size)
          if SURROGATES.cover?(point) && point >= 0xDC00 && (last = points.last) && last.between?(0xD800, 0xDBFF)
            points[-1] = 0x10000 + ((last - 0xD800) << 10) + (point - 0xDC00)
          else
            points << point
          end
          index += used
        end
        points
      end

      def continuation?(bytes, index)
        byte = bytes.getbyte(index)
        !byte.nil? && (byte & 0xC0) == 0x80
      end

      def decode_one(bytes, index, size)
        lead = bytes.getbyte(index)
        return [lead, 1] if lead < 0x80

        if lead.between?(0xC2, 0xDF) && index + 1 < size && continuation?(bytes, index + 1)
          return [((lead & 0x1F) << 6) | (bytes.getbyte(index + 1) & 0x3F), 2]
        end

        if lead.between?(0xE0,
                         0xEF) && index + 2 < size && continuation?(bytes, index + 1) && continuation?(bytes, index + 2)
          point = ((lead & 0x0F) << 12) | ((bytes.getbyte(index + 1) & 0x3F) << 6) | (bytes.getbyte(index + 2) & 0x3F)
          return [point, 3] if point >= 0x800
        end
        if lead.between?(0xF0, 0xF4) && index + 3 < size && (1..3).all? { |step| continuation?(bytes, index + step) }
          point = ((lead & 0x07) << 18) | ((bytes.getbyte(index + 1) & 0x3F) << 12) |
                  ((bytes.getbyte(index + 2) & 0x3F) << 6) | (bytes.getbyte(index + 3) & 0x3F)
          return [point, 4] if point.between?(0x10000, 0x10FFFF)
        end
        [REPLACEMENT, 1]
      end
    end
  end
end
