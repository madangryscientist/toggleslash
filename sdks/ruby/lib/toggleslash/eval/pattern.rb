# frozen_string_literal: true

require_relative 'pattern_parser'

module ToggleSlash
  module Eval
    # Patterns for the `matches` op, to the "Patterns" section of docs/evaluation.md. `PatternParser` reads a
    # pattern into a tree; this compiles the tree to a small program and runs it on a Pike VM over code
    # points: no backtracking and no Regexp, so a match costs O(program x input) whatever the pattern.
    module Pattern
      # `op` is :char, :split, :start, :end or :match. A jump is a split whose two targets are the same.
      Inst = Struct.new(:op, :set, :x, :y)

      module_function

      def split(program, target)
        inst = Inst.new(:split, nil, target, target)
        program << inst
        inst
      end

      def emit_alt(sides, program)
        jumps = []
        last = sides.length - 1
        sides.each_with_index do |side, index|
          choice = index < last ? split(program, program.length + 1) : nil
          emit(side, program)
          next if choice.nil?

          jumps << split(program, 0)
          choice.y = program.length
        end
        jumps.each { |jump| jump.x = jump.y = program.length }
      end

      def emit_repeat(body, min, max, program)
        # A body of size 0 is empty groups only and matches just the empty string, so repeating it emits nothing.
        return if body.size.zero? # rubocop:disable Style/ZeroLengthPredicate

        min.times { emit(body, program) }
        if max.nil?
          top = program.length
          loop_split = split(program, top + 1)
          emit(body, program)
          split(program, top)
          loop_split.y = program.length
          return
        end
        optional = []
        (max - min).times do
          optional << split(program, program.length + 1)
          emit(body, program)
        end
        optional.each { |choice| choice.y = program.length }
      end

      def emit(node, program)
        case node.kind
        when :set then program << Inst.new(:char, node.set)
        when :start, :end then program << Inst.new(node.kind)
        when :concat then node.parts.each { |part| emit(part, program) }
        when :alt then emit_alt(node.parts, program)
        when :repeat then emit_repeat(node.body, node.min, node.max, program)
        end
      end

      # The compiled pattern, or nil when `pattern` is not a valid pattern.
      def compile(pattern)
        node = PatternParser.parse(Text.codepoints(pattern))
        return nil if node.nil?

        program = []
        emit(node, program)
        program << Inst.new(:match)
        program
      end

      def in_item?(item, point)
        ranges = item.ranges
        hit = false
        index = 0
        while index + 1 < ranges.length && !hit
          hit = point.between?(ranges[index], ranges[index + 1])
          index += 2
        end
        hit != item.negate
      end

      def in_set?(set, point)
        set.items.any? { |item| in_item?(item, point) } != set.negate
      end

      # Does the pattern match somewhere in `text`? A Pike VM: each instruction is visited once per position.
      def matches?(program, text)
        input = Text.codepoints(text)
        length = input.length
        seen = Array.new(program.length, -1)
        stack = []
        current = []
        following = []

        # Adds `pc` and everything it reaches without reading to `list`; true when that reaches a match.
        add = lambda do |list, start, pos|
          stack.push(start)
          until stack.empty?
            at = stack.pop
            inst = program[at]
            next if inst.nil? || seen[at] == pos

            seen[at] = pos
            case inst.op
            when :match
              stack.clear
              return true
            when :char then list << at
            when :split then stack.push(inst.y, inst.x)
            when :start then stack.push(at + 1) if pos.zero?
            when :end then stack.push(at + 1) if pos == length
            end
          end
          false
        end

        pos = 0
        loop do
          return true if add.call(current, 0, pos)

          point = input[pos]
          return false if point.nil?

          following.clear
          current.each do |pc|
            inst = program[pc]
            return true if inst.op == :char && in_set?(inst.set, point) && add.call(following, pc + 1, pos + 1)
          end
          current, following = following, current
          pos += 1
        end
      end
    end
  end
end
