# frozen_string_literal: true

module Toggleslash
  module Eval
    # The `matches` dialect of evaluation.md, "Patterns": a parser to a small program and a Pike VM.
    # No Regexp of the platform is used. A pattern and a candidate are Arrays of code points.
    module Pattern
      MAX_LENGTH = 1024
      MAX_SIZE = 10_000
      MAX_COUNT = 100
      MAX_CODE_POINT = 0x10FFFF

      META = "\\.[]()|*+?{}^$".bytes.freeze
      SIMPLE_ESCAPES = { 110 => 10, 116 => 9, 114 => 13, 102 => 12, 118 => 11 }.freeze # n t r f v
      DIGIT = (48..57).to_a.freeze

      DIGIT_RANGES = [[48, 57]].freeze
      WORD_RANGES = [[48, 57], [65, 90], [95, 95], [97, 122]].freeze
      SPACE_RANGES = [[9, 13], [32, 32]].freeze

      def self.complement(ranges)
        out = []
        low = 0
        ranges.sort.each do |lo, hi|
          out << [low, lo - 1] if lo > low
          low = hi + 1
        end
        out << [low, MAX_CODE_POINT] if low <= MAX_CODE_POINT
        out.freeze
      end

      CLASS_ESCAPES = {
        100 => DIGIT_RANGES, 119 => WORD_RANGES, 115 => SPACE_RANGES, # d w s
        68 => complement(DIGIT_RANGES), 87 => complement(WORD_RANGES), 83 => complement(SPACE_RANGES) # D W S
      }.freeze

      class Invalid < StandardError; end

      # The parser. Nodes are Arrays: [:lit, cp] [:any] [:class, negated, ranges] [:bol] [:eol]
      # [:cat, nodes] [:alt, nodes] [:rep, node, low, high_or_nil, simple].
      class Parser
        def initialize(points)
          @t = points
          @i = 0
          @n = points.size
        end

        def parse
          node = alternation
          raise Invalid if @i < @n # an unmatched ")"

          node
        end

        private

        def at?(*codes)
          @i < @n && codes.include?(@t[@i])
        end

        def alternation
          sides = [concatenation]
          while at?(124) # |
            @i += 1
            sides << concatenation
          end
          sides.size == 1 ? sides[0] : [:alt, sides]
        end

        def concatenation
          items = []
          while @i < @n && !at?(124, 41) # | )
            node, quantifiable = atom
            if at?(42, 43, 63, 123) # * + ? {
              raise Invalid unless quantifiable

              simple = !at?(123)
              low, high = quantifier
              node = [:rep, node, low, high, simple]
              raise Invalid if at?(42, 43, 63, 123) # a quantifier after a quantifier
            end
            items << node
          end
          [:cat, items]
        end

        def atom
          ch = @t[@i]
          raise Invalid if [42, 43, 63, 123, 125, 93].include?(ch) # * + ? { } ]

          case ch
          when 40 then group
          when 91 then [char_class, true]
          when 46 then advance([[:any], true])
          when 94 then advance([[:bol], false])
          when 36 then advance([[:eol], false])
          when 92 then escape_atom
          else advance([[:lit, ch], true])
          end
        end

        def advance(value)
          @i += 1
          value
        end

        def group
          @i += 1
          raise Invalid if at?(63) # (?

          inner = alternation
          raise Invalid unless at?(41)

          @i += 1
          [inner, true]
        end

        def escape_atom
          esc = escape
          esc.is_a?(Integer) ? [[:lit, esc], true] : [[:class, false, esc], true]
        end

        # `\x` at the cursor: a code point, or the ranges of d w s D W S.
        def escape
          raise Invalid if @i + 1 >= @n

          ch = @t[@i + 1]
          @i += 2
          return ch if META.include?(ch) || ch == 45
          return SIMPLE_ESCAPES[ch] if SIMPLE_ESCAPES.key?(ch)
          return CLASS_ESCAPES[ch] if CLASS_ESCAPES.key?(ch)

          raise Invalid
        end

        def number
          start = @i
          @i += 1 while @i < @n && DIGIT.include?(@t[@i])
          digits = @t[start...@i]
          raise Invalid if digits.empty? || (digits.size > 1 && digits[0] == 48) || digits.size > 3

          value = digits.reduce(0) { |acc, d| (acc * 10) + (d - 48) }
          raise Invalid if value > MAX_COUNT

          value
        end

        def quantifier
          ch = @t[@i]
          @i += 1
          return [0, nil] if ch == 42
          return [1, nil] if ch == 43
          return [0, 1] if ch == 63

          low = number
          high = low
          if at?(44) # ,
            @i += 1
            if at?(125)
              high = nil
            else
              high = number
              raise Invalid if high < low
            end
          end
          raise Invalid unless at?(125)

          @i += 1
          [low, high]
        end

        def class_atom(first)
          ch = @t[@i]
          raise Invalid if ch == 91
          return escape if ch == 92

          if ch == 45
            before_close = @i + 1 < @n && @t[@i + 1] == 93
            raise Invalid unless first || before_close
          end
          @i += 1
          ch
        end

        def char_class
          @i += 1 # [
          negated = false
          if at?(94)
            negated = true
            @i += 1
          end
          ranges = []
          first = true
          loop do
            raise Invalid if @i >= @n

            if @t[@i] == 93
              raise Invalid if first

              @i += 1
              break
            end
            member = class_atom(first)
            first = false
            if @i + 1 < @n && @t[@i] == 45 && @t[@i + 1] != 93
              @i += 1
              upper = class_atom(false)
              raise Invalid unless member.is_a?(Integer) && upper.is_a?(Integer) && member <= upper

              ranges << [member, upper]
            elsif member.is_a?(Integer)
              ranges << [member, member]
            else
              ranges.concat(member)
            end
          end
          [:class, negated, ranges.freeze]
        end
      end

      def self.size(node)
        case node[0]
        when :cat then node[1].sum { |part| size(part) }
        when :alt then node[1].sum { |part| size(part) } + node[1].size - 1
        when :rep then repeat_size(node)
        else 1
        end
      end

      def self.repeat_size(node)
        body = size(node[1])
        low = node[2]
        high = node[3]
        total =
          if node[4]
            body + 1
          elsif high.nil?
            (low + 1) * body
          else
            high * body
          end
        [1, total].max
      end

      # Opcodes.
      CHAR = 0
      ANY = 1
      CLASS = 2
      SPLIT = 3
      JMP = 4
      BOL = 5
      EOL = 6
      MATCH = 7

      def self.emit(node, prog)
        case node[0]
        when :lit then prog << [CHAR, node[1]]
        when :any then prog << [ANY]
        when :class then prog << [CLASS, node[1], node[2]]
        when :bol then prog << [BOL]
        when :eol then prog << [EOL]
        when :cat then node[1].each { |part| emit(part, prog) }
        when :alt then emit_alt(node[1], prog)
        else emit_rep(node, prog)
        end
      end

      def self.emit_alt(sides, prog)
        jumps = []
        sides.each_with_index do |side, index|
          if index < sides.size - 1
            split = prog.size
            prog << [SPLIT, split + 1, -1]
            emit(side, prog)
            jumps << prog.size
            prog << [JMP, -1]
            prog[split] = [SPLIT, split + 1, prog.size]
          else
            emit(side, prog)
          end
        end
        finish = prog.size
        jumps.each { |j| prog[j] = [JMP, finish] }
      end

      def self.emit_rep(node, prog)
        body = node[1]
        low = node[2]
        high = node[3]
        return if size(body).zero? # an empty body matches the empty string and repeating it changes nothing

        low.times { emit(body, prog) }
        if high.nil?
          split = prog.size
          prog << [SPLIT, split + 1, -1]
          emit(body, prog)
          prog << [JMP, split]
          prog[split] = [SPLIT, split + 1, prog.size]
        else
          splits = []
          (high - low).times do
            splits << prog.size
            prog << [SPLIT, prog.size + 1, -1]
            emit(body, prog)
          end
          finish = prog.size
          splits.each { |s| prog[s] = [SPLIT, s + 1, finish] }
        end
      end

      CACHE = {} # rubocop:disable Style/MutableConstant -- filled at run time, under CACHE_LOCK
      CACHE_LOCK = Mutex.new
      CACHE_LIMIT = 512

      # The compiled program for a pattern given as code points, or nil when it is not valid.
      def self.compile(points)
        return nil if points.size > MAX_LENGTH

        key = points.pack("U*") if points.none? { |cp| cp >= 0xD800 && cp <= 0xDFFF }
        if key
          hit = CACHE_LOCK.synchronize { CACHE[key] }
          return hit || nil unless hit.nil?
        end
        prog = build(points)
        if key
          CACHE_LOCK.synchronize do
            CACHE.clear if CACHE.size >= CACHE_LIMIT
            CACHE[key] = prog || false
          end
        end
        prog || nil
      end

      def self.build(points)
        tree = Parser.new(points).parse
        return nil if size(tree) > MAX_SIZE

        prog = []
        emit(tree, prog)
        prog << [MATCH]
        prog.freeze
      rescue Invalid
        nil
      end

      def self.class_has?(ranges, cp)
        ranges.any? { |lo, hi| cp >= lo && cp <= hi }
      end

      # Follows jumps and splits from `start` at input position `pos`, adding the instructions that read a
      # code point to `threads`. Returns true when MATCH is reached.
      def self.add(prog, mark, threads, start, pos, length)
        stack = [start]
        until stack.empty?
          pc = stack.pop
          next if mark[pc] == pos

          mark[pc] = pos
          ins = prog[pc]
          case ins[0]
          when JMP then stack << ins[1]
          when SPLIT
            stack << ins[2]
            stack << ins[1]
          when BOL then stack << (pc + 1) if pos.zero?
          when EOL then stack << (pc + 1) if pos == length
          when MATCH then return true
          else threads << pc
          end
        end
        false
      end

      # True when the program matches somewhere in `text` (an Array of code points). Each instruction runs
      # at most once per input position, so the time is bounded by size x input whatever the pattern.
      def self.search(prog, text)
        length = text.size
        mark = Array.new(prog.size, -1)
        current = []
        pos = 0
        while pos <= length
          return true if add(prog, mark, current, 0, pos, length)
          break if pos == length

          cp = text[pos]
          following = []
          current.each do |pc|
            ins = prog[pc]
            ok =
              case ins[0]
              when CHAR then ins[1] == cp
              when ANY then cp != 10
              else class_has?(ins[2], cp) != ins[1]
              end
            return true if ok && add(prog, mark, following, pc + 1, pos + 1, length)
          end
          current = following
          pos += 1
        end
        false
      end
    end
  end
end
