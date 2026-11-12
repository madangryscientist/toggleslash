# frozen_string_literal: true

module Toggleslash
  module Eval
    # Attribute references: "/a/b~1c" (evaluation.md, "Reading the attribute").
    module Reference
      # Splits a name that starts with "/" into its decoded components, or nil if it is invalid.
      def self.parse(name)
        out = []
        rest = name.byteslice(1, name.bytesize - 1)
        (rest.empty? ? [rest] : rest.split("/", -1)).each do |part|
          decoded = decode(part)
          return nil if decoded.nil?

          out << decoded
        end
        out
      end

      def self.decode(part)
        return nil if part.empty?

        out = +"".b
        bytes = part.bytes
        i = 0
        while i < bytes.size
          byte = bytes[i]
          if byte != 126 # ~
            out << byte
            i += 1
            next
          end
          nxt = bytes[i + 1]
          case nxt
          when 48 then out << 126
          when 49 then out << 47
          else return nil
          end
          i += 2
        end
        out.force_encoding(Encoding::UTF_8)
      end

      # The components an attribute name reads, or nil if the name is not valid. A name that does not start
      # with "/" is one literal component.
      def self.path(name)
        return nil unless name.is_a?(String) && !name.empty?

        name = Text.utf8(name)
        name.getbyte(0) == 47 ? parse(name) : [name]
      end
    end
  end
end
