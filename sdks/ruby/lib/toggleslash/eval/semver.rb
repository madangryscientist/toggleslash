# frozen_string_literal: true

module Toggleslash
  module Eval
    # Semantic versions: a hand-written walk over the bytes (evaluation.md, "Semantic versions").
    module Semver
      DIGITS = (48..57).to_a.freeze # 0-9
      IDENT = (DIGITS + (65..90).to_a + (97..122).to_a + [45]).freeze # 0-9 A-Z a-z -

      # A parsed version: [major, minor, patch, [prerelease identifiers]] with every part a String.
      def self.numeric?(part)
        !part.empty? && part.bytes.all? { |b| DIGITS.include?(b) } && (part == "0" || part.getbyte(0) != 48)
      end

      def self.identifiers(text)
        return nil if text.empty?

        parts = text.split(".", -1)
        return nil if parts.any? { |part| part.empty? || !part.bytes.all? { |b| IDENT.include?(b) } }

        parts
      end

      def self.digits_only?(text)
        text.bytes.all? { |b| DIGITS.include?(b) }
      end

      # The parsed version, or nil when `text` is not one.
      def self.parse(text)
        rest = text
        build = nil
        plus = rest.index("+")
        if plus
          build = rest[(plus + 1)..]
          rest = rest[0, plus]
          return nil if identifiers(build).nil?
        end
        pre = nil
        dash = rest.index("-")
        if dash
          pre = identifiers(rest[(dash + 1)..])
          rest = rest[0, dash]
          return nil if pre.nil?
          return nil if pre.any? { |id| digits_only?(id) && id.size > 1 && id.getbyte(0) == 48 }
        end
        core = core_parts(rest, !build.nil? || !pre.nil?)
        return nil if core.nil? || !core.all? { |part| numeric?(part) }

        [core[0], core[1], core[2], pre || []]
      end

      def self.core_parts(rest, has_suffix)
        core = rest.split(".", -1)
        return core if core.size == 3
        return core + (["0"] * (3 - core.size)) if [1, 2].include?(core.size) && !has_suffix

        nil
      end

      def self.compare_numeric(left, right)
        return left.size <=> right.size if left.size != right.size

        left <=> right
      end

      # -1, 0 or 1 by SemVer 2.0.0 section 11 (build is not part of a parsed version).
      def self.compare(left, right)
        3.times do |i|
          c = compare_numeric(left[i], right[i])
          return c unless c.zero?
        end
        pre_left = left[3]
        pre_right = right[3]
        return 0 if pre_left.empty? && pre_right.empty?
        return 1 if pre_left.empty?
        return -1 if pre_right.empty?

        [pre_left.size, pre_right.size].min.times do |i|
          c = compare_identifier(pre_left[i], pre_right[i])
          return c unless c.zero?
        end
        pre_left.size <=> pre_right.size
      end

      def self.compare_identifier(left, right)
        left_digits = digits_only?(left)
        right_digits = digits_only?(right)
        if left_digits && right_digits
          compare_numeric(left, right)
        elsif left_digits
          -1
        elsif right_digits
          1
        else
          left <=> right
        end
      end
    end
  end
end
