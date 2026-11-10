# frozen_string_literal: true

require_relative 'text'

module ToggleSlash
  module Eval
    # The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in, a
    # tree with each node's size out, or nil for a pattern that is not valid. `Pattern` compiles the tree and
    # runs it.
    module PatternParser
      MAX_LENGTH = 1024
      MAX_SIZE = 10_000
      MAX_COUNT = 100
      END_OF_INPUT = -1

      # Code point ranges as a flat list `lo, hi, lo, hi`, inclusive; the complement when `negate` is set.
      Item = Struct.new(:ranges, :negate)
      # A class: any of its items, or none of them when `negate` is set.
      CharSet = Struct.new(:items, :negate)
      # `kind` is :set, :start, :end, :concat, :alt or :repeat.
      Node = Struct.new(:kind, :size, :set, :parts, :body, :min, :max)

      class Invalid < StandardError; end

      DIGIT = Item.new([0x30, 0x39].freeze, false).freeze
      WORD = Item.new([0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A].freeze, false).freeze
      # U+0009 to U+000D are \t \n \v \f \r.
      SPACE = Item.new([0x09, 0x0D, 0x20, 0x20].freeze, false).freeze
      NEWLINE = 0x0A
      ESCAPABLE = '\\.[]()|*+?{}^$-'.codepoints.freeze
      ESCAPES = {
        'n'.ord => 0x0A, 't'.ord => 0x09, 'r'.ord => 0x0D, 'f'.ord => 0x0C, 'v'.ord => 0x0B,
        'd'.ord => DIGIT, 'w'.ord => WORD, 's'.ord => SPACE,
        'D'.ord => Item.new(DIGIT.ranges, true).freeze,
        'W'.ord => Item.new(WORD.ranges, true).freeze,
        'S'.ord => Item.new(SPACE.ranges, true).freeze
      }.freeze
      NOT_AN_ATOM = '*+?{}]'.codepoints.freeze
      QUANTIFIERS = '*+?{'.codepoints.freeze
      PIPE = '|'.ord
      CLOSE = ')'.ord
      OPEN = '('.ord
      LBRACKET = '['.ord
      RBRACKET = ']'.ord
      BACKSLASH = '\\'.ord
      CARET = '^'.ord
      DOLLAR = '$'.ord
      DOT = '.'.ord
      DASH = '-'.ord
      COMMA = ','.ord
      LBRACE = '{'.ord
      RBRACE = '}'.ord
      PLUS = '+'.ord
      QUESTION = '?'.ord
      ZERO = '0'.ord

      # One pattern's parse.
      class Parser
        def initialize(points)
          @points = points
          @pos = 0
        end

        def parse
          node = alternation
          fail_parse if @pos < @points.length # an unmatched `)`
          node
        end

        private

        def fail_parse
          raise Invalid
        end

        def peek(offset = 0)
          @points[@pos + offset] || END_OF_INPUT
        end

        def take
          point = peek
          @pos += 1
          point
        end

        def saturate(size)
          [size, MAX_SIZE + 1].min
        end

        def digit?(point)
          point.between?(0x30, 0x39)
        end

        def single(point)
          Item.new([point, point], false)
        end

        def single_set(item)
          Node.new(:set, 1, CharSet.new([item], false))
        end

        def as_item(atom)
          atom.is_a?(Integer) ? single(atom) : atom
        end

        def alternation
          sides = [concat]
          while peek == PIPE
            @pos += 1
            sides << concat
          end
          return sides.first if sides.length == 1

          Node.new(:alt, saturate(sides.sum(&:size) + sides.length - 1), nil, sides)
        end

        def concat
          parts = []
          until [END_OF_INPUT, PIPE, CLOSE].include?(peek)
            piece = atom
            anchor = %i[start end].include?(piece.kind)
            parts << (!anchor && QUANTIFIERS.include?(peek) ? quantify(piece) : piece)
          end
          Node.new(:concat, saturate(parts.sum(&:size)), nil, parts)
        end

        def atom
          point = take
          return group if point == OPEN
          return Node.new(:set, 1, char_class) if point == LBRACKET
          return single_set(as_item(escape)) if point == BACKSLASH

          fail_parse if NOT_AN_ATOM.include?(point)
          return Node.new(:set, 1, CharSet.new([Item.new([NEWLINE, NEWLINE], true)], false)) if point == DOT
          return Node.new(:start, 1) if point == CARET
          return Node.new(:end, 1) if point == DOLLAR

          single_set(single(point))
        end

        def group
          inner = alternation
          fail_parse unless take == CLOSE
          inner
        end

        # After a `\`: one code point, or one of the six shorthand classes.
        def escape
          point = take
          return point if ESCAPABLE.include?(point)

          ESCAPES.fetch(point) { fail_parse }
        end

        # After `[`: the members up to `]`.
        def char_class
          negate = peek == CARET
          @pos += 1 if negate
          items = [member(true)]
          items << member(false) until peek == RBRACKET
          @pos += 1
          CharSet.new(items, negate)
        end

        # One member: an atom, or a range of two single code points.
        def member(first)
          from = class_atom(first)
          range = peek == DASH && peek(1) != END_OF_INPUT && peek(1) != RBRACKET
          return as_item(from) unless range

          @pos += 1
          to = class_atom(false)
          fail_parse if !from.is_a?(Integer) || !to.is_a?(Integer) || from > to
          Item.new([from, to], false)
        end

        def class_atom(first)
          point = take
          # `]` here is a class with no members; the end and `[` are never members.
          fail_parse if [END_OF_INPUT, LBRACKET, RBRACKET].include?(point)
          return escape if point == BACKSLASH

          # An unescaped `-` is a literal only as the first member or directly before `]`.
          fail_parse if point == DASH && !first && peek != RBRACKET

          point
        end

        def quantify(body)
          point = take
          if point != LBRACE
            min = point == PLUS ? 1 : 0
            max = point == QUESTION ? 1 : nil
            return Node.new(:repeat, saturate(body.size + 1), nil, nil, body, min, max)
          end
          min = count
          max = upper(min)
          fail_parse unless take == RBRACE
          return repeat(body, min, nil, min + 1) if max.nil?

          fail_parse if min > max
          repeat(body, min, max, max)
        end

        # After `{n`: n again for `{n}`, nil for `{n,}`, m for `{n,m}`. Leaves the `}`.
        def upper(min)
          return min unless peek == COMMA

          @pos += 1
          peek == RBRACE ? nil : count
        end

        def repeat(body, min, max, times)
          Node.new(:repeat, [1, saturate(times * body.size)].max, nil, nil, body, min, max)
        end

        # ASCII digits with no leading zero, at most 100.
        def count
          point = take
          fail_parse unless digit?(point)
          if point == ZERO
            fail_parse if digit?(peek)
            return 0
          end
          value = point - 0x30
          while digit?(peek)
            value = (value * 10) + (take - 0x30)
            fail_parse if value > MAX_COUNT
          end
          value
        end
      end

      module_function

      # The tree for a pattern's code points, or nil when it is not a valid pattern: too long, malformed or too big.
      def parse(points)
        return nil if points.length > MAX_LENGTH

        node = Parser.new(points).parse
        node.size > MAX_SIZE ? nil : node
      rescue Invalid
        nil
      end
    end
  end
end
