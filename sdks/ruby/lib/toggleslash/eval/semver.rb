# frozen_string_literal: true

module ToggleSlash
  module Eval
    # Semantic versions, to the "Semantic versions" section of docs/evaluation.md. A hand-written walk over
    # the string: no regular expression and no library. Nothing here converts a numeric part to a number.
    module Semver
      Version = Struct.new(:major, :minor, :patch, :prerelease)

      module_function

      def digit?(byte)
        byte.between?(0x30, 0x39)
      end

      def identifier_byte?(byte)
        digit?(byte) || byte.between?(0x41, 0x5A) || byte.between?(0x61, 0x7A) || byte == 0x2D
      end

      def all_digits?(text)
        text.each_byte.all? { |byte| digit?(byte) }
      end

      # `0` or digits with no leading zero.
      def numeric_part?(text)
        !text.empty? && all_digits?(text) && (text == '0' || text.getbyte(0) != 0x30)
      end

      # Dot-separated, none empty, each of ASCII letters, digits and `-`.
      def identifiers(text)
        return nil if text.empty?

        parts = text.split('.', -1)
        parts.each do |part|
          return nil if part.empty?
          return nil unless part.each_byte.all? { |byte| identifier_byte?(byte) }
        end
        parts
      end

      # Text before and after the first `char`; nil after when there is none.
      def split_at(text, char)
        at = text.index(char)
        at ? [text[0, at], text[(at + 1)..]] : [text, nil]
      end

      def prerelease_identifiers(text)
        parts = identifiers(text)
        return nil if parts.nil?

        parts.all? { |part| !all_digits?(part) || numeric_part?(part) } ? parts : nil
      end

      def numeric_parts(text, short)
        return nil if text.empty?

        numbers = text.split('.', -1)
        return nil if numbers.length > 3 || (numbers.length < 3 && !short)

        numbers.all? { |part| numeric_part?(part) } ? numbers : nil
      end

      # The version `text` spells, or nil when it is not one.
      def parse(text)
        return nil unless text.ascii_only?

        without_build, build = split_at(text, '+')
        return nil if !build.nil? && identifiers(build).nil?

        core, prerelease = split_at(without_build, '-')
        parsed = prerelease.nil? ? [] : prerelease_identifiers(prerelease)
        return nil if parsed.nil?

        numbers = numeric_parts(core, build.nil? && prerelease.nil?)
        return nil if numbers.nil?

        Version.new(numbers[0] || '0', numbers[1] || '0', numbers[2] || '0', parsed)
      end

      # Digit strings with no leading zero: the longer is larger, else by code unit.
      def compare_numeric(left, right)
        return left.length <=> right.length if left.length != right.length

        left <=> right
      end

      def compare_identifier(left, right)
        left_numeric = all_digits?(left)
        right_numeric = all_digits?(right)
        return compare_numeric(left, right) if left_numeric && right_numeric
        return -1 if left_numeric
        return 1 if right_numeric

        left <=> right
      end

      def compare_prerelease(left, right)
        # No prerelease is above any prerelease of the same core.
        return right.length <=> left.length if left.empty? || right.empty?

        [left.length, right.length].min.times do |index|
          order = compare_identifier(left[index], right[index])
          return order unless order.zero?
        end
        left.length <=> right.length
      end

      # -1, 0 or 1 by SemVer 2.0.0 section 11; build metadata is already gone.
      def compare(left, right)
        %i[major minor patch].each do |part|
          order = compare_numeric(left[part], right[part])
          return order unless order.zero?
        end
        compare_prerelease(left.prerelease, right.prerelease)
      end
    end
  end
end
