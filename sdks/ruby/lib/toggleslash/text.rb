# frozen_string_literal: true

module Toggleslash
  # String helpers for the evaluator. Strings are UTF-8 `String`s that may hold a lone surrogate in its
  # three-byte generalised form (WTF-8), so they can be invalid to Ruby. The spec compares and orders by
  # UTF-16 code unit and matches patterns by code point; hashing replaces a lone surrogate with U+FFFD.
  module Text
    module_function

    # The string as UTF-8. Another encoding is converted; binary and unknown bytes are read as UTF-8.
    def utf8(str)
      enc = str.encoding
      return str if enc == Encoding::UTF_8
      return str.dup.force_encoding(Encoding::UTF_8) if [Encoding::BINARY, Encoding::US_ASCII].include?(enc)

      str.encode(Encoding::UTF_8, invalid: :replace, undef: :replace, replace: "�")
    rescue StandardError
      str.dup.force_encoding(Encoding::UTF_8)
    end

    # UTF-16 code units of the string, as an Array of Integers.
    def units(str)
      return str.bytes if str.ascii_only?
      return decode(str) unless str.valid_encoding?

      code_points = str.unpack("U*")
      return code_points unless code_points.any? { |cp| cp > 0xFFFF }

      split_pairs(code_points)
    end

    # Code points of the string: a valid surrogate pair is one code point, a lone surrogate is one too.
    def code_points(str)
      return str.bytes if str.ascii_only?
      return str.unpack("U*") if str.valid_encoding?

      join_pairs(decode(str))
    end

    # The string as UTF-8 bytes for hashing: a lone surrogate (and any invalid byte) becomes U+FFFD.
    def hash_bytes(str)
      return str.b if str.valid_encoding?

      join_pairs(decode(str)).map { |cp| cp >= 0xD800 && cp <= 0xDFFF ? 0xFFFD : cp }.pack("U*").b
    end

    # A binary string of the code units, big-endian, so a byte comparison is a code unit comparison.
    def unit_key(str)
      units(str).pack("n*")
    end

    def same?(left, right)
      return true if left == right
      return false if left.valid_encoding? && right.valid_encoding?

      unit_key(left) == unit_key(right)
    end

    def contains?(hay, needle)
      return hay.include?(needle) if hay.valid_encoding? && needle.valid_encoding?

      haystack = unit_key(hay)
      target = unit_key(needle)
      pos = 0
      while (idx = haystack.index(target, pos))
        return true if idx.even?

        pos = idx + 1
      end
      false
    end

    def starts_with?(str, prefix)
      return str.start_with?(prefix) if str.valid_encoding? && prefix.valid_encoding?

      unit_key(str).start_with?(unit_key(prefix))
    end

    def ends_with?(str, suffix)
      return str.end_with?(suffix) if str.valid_encoding? && suffix.valid_encoding?

      unit_key(str).end_with?(unit_key(suffix))
    end

    # -1, 0 or 1 by UTF-16 code unit.
    def compare(left, right)
      return left <=> right if left.ascii_only? && right.ascii_only?

      unit_key(left) <=> unit_key(right)
    end

    # A WHATWG UTF-8 decode of the bytes into UTF-16 code units, with the one extension that a
    # three-byte surrogate (ED A0..BF xx) is read as the code unit it stands for.
    def decode(str)
      bytes = str.bytes
      out = []
      i = 0
      count = bytes.size
      while i < count
        byte = bytes[i]
        i += 1
        if byte < 0x80
          out << byte
          next
        end
        need, low, high, cp = lead(byte)
        if need.nil?
          out << 0xFFFD
          next
        end
        ok = true
        need.times do
          nb = bytes[i]
          if nb && nb >= low && nb <= high
            cp = (cp << 6) | (nb & 0x3F)
            i += 1
            low = 0x80
            high = 0xBF
          else
            ok = false
            break
          end
        end
        if ok
          push_unit(out, cp)
        else
          out << 0xFFFD
        end
      end
      out
    end

    # [continuation bytes needed, lowest and highest first continuation byte, payload], or nil.
    def lead(byte)
      if byte >= 0xC2 && byte <= 0xDF
        [1, 0x80, 0xBF, byte & 0x1F]
      elsif byte >= 0xE0 && byte <= 0xEF
        [2, byte == 0xE0 ? 0xA0 : 0x80, 0xBF, byte & 0x0F]
      elsif byte >= 0xF0 && byte <= 0xF4
        [3, byte == 0xF0 ? 0x90 : 0x80, byte == 0xF4 ? 0x8F : 0xBF, byte & 0x07]
      end
    end

    def push_unit(out, cp)
      if cp > 0xFFFF
        cp -= 0x10000
        out << (0xD800 + (cp >> 10))
        out << (0xDC00 + (cp & 0x3FF))
      else
        out << cp
      end
    end

    def split_pairs(code_points)
      out = []
      code_points.each { |cp| push_unit(out, cp) }
      out
    end

    def join_pairs(units)
      out = []
      i = 0
      count = units.size
      while i < count
        unit = units[i]
        nxt = units[i + 1]
        if unit >= 0xD800 && unit <= 0xDBFF && nxt && nxt >= 0xDC00 && nxt <= 0xDFFF
          out << (0x10000 + ((unit - 0xD800) << 10) + (nxt - 0xDC00))
          i += 2
        else
          out << unit
          i += 1
        end
      end
      out
    end
  end
end
