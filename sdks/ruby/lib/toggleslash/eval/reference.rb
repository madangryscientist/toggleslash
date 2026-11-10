# frozen_string_literal: true

module ToggleSlash
  module Eval
    # An attribute reference, to "Reading the attribute" in docs/evaluation.md: a name that starts with `/`.
    # The components are what is left after dropping the leading `/`, splitting on `/` and decoding `~1` as
    # `/` and `~0` as `~` in one left-to-right pass. Nil when a component is empty or a `~` is followed by
    # anything but `0` or `1`, or ends the component. Bytes only: every character the format cares about is
    # ASCII, so a lone surrogate passes through untouched.
    module Reference
      SLASH = 0x2F
      TILDE = 0x7E

      module_function

      def parse(text)
        bytes = text.b
        components = []
        current = ''.b
        index = 1
        size = bytes.bytesize
        while index <= size
          byte = index < size ? bytes.getbyte(index) : SLASH
          if byte == SLASH
            return nil if current.empty?

            components << current.force_encoding(Encoding::UTF_8)
            current = ''.b
          elsif byte == TILDE
            escape = bytes.getbyte(index + 1)
            return nil unless [0x30, 0x31].include?(escape)

            current << (escape == 0x30 ? TILDE : SLASH)
            index += 1
          else
            current << byte
          end
          index += 1
        end
        components
      end
    end
  end
end
