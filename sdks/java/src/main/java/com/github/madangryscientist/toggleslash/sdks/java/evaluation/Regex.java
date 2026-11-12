package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * Patterns for the {@code matches} op, to the "Patterns" section of docs/evaluation.md. {@link
 * PatternParser} reads a pattern into a tree; this compiles the tree to a small program and runs it
 * on a Pike VM over code points: no backtracking and no platform regular expression, so a match
 * costs O(program x input) whatever the pattern.
 */
final class Regex {
  private enum Op {
    CHAR,
    SPLIT,
    START,
    END,
    MATCH
  }

  /** One instruction. A jump is a split whose two targets are the same. */
  private static final class Inst {
    final Op op;
    final PatternParser.CharSet set;
    int x;
    int y;

    Inst(Op op, PatternParser.CharSet set, int x) {
      this.op = op;
      this.set = set;
      this.x = x;
      this.y = x;
    }
  }

  private final Inst[] program;

  private Regex(Inst[] program) {
    this.program = program;
  }

  /** The compiled pattern, or null when {@code pattern} is not a valid pattern. */
  static Regex compile(String pattern) {
    PatternParser.Node node = PatternParser.parse(pattern.codePoints().toArray());
    if (node == null) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(Op.MATCH, null, 0));
    return new Regex(program.toArray(new Inst[0]));
  }

  private static Inst split(List<Inst> program, int x) {
    Inst inst = new Inst(Op.SPLIT, null, x);
    program.add(inst);
    return inst;
  }

  private static void emitAlt(List<PatternParser.Node> sides, List<Inst> program) {
    List<Inst> jumps = new ArrayList<>();
    int last = sides.size() - 1;
    for (int i = 0; i <= last; i++) {
      Inst choice = i < last ? split(program, program.size() + 1) : null;
      emit(sides.get(i), program);
      if (choice == null) {
        continue;
      }
      jumps.add(split(program, 0));
      choice.y = program.size();
    }
    for (Inst jump : jumps) {
      jump.x = program.size();
      jump.y = program.size();
    }
  }

  private static void emitRepeat(PatternParser.Node body, int min, int max, List<Inst> program) {
    // A body of size 0 is empty groups only and matches just the empty string, so repeating it
    // emits nothing.
    if (body.size() == 0) {
      return;
    }
    for (int i = 0; i < min; i++) {
      emit(body, program);
    }
    if (max == -1) {
      int top = program.size();
      Inst loop = split(program, top + 1);
      emit(body, program);
      split(program, top);
      loop.y = program.size();
      return;
    }
    List<Inst> optional = new ArrayList<>();
    for (int i = min; i < max; i++) {
      optional.add(split(program, program.size() + 1));
      emit(body, program);
    }
    for (Inst choice : optional) {
      choice.y = program.size();
    }
  }

  private static void emit(PatternParser.Node node, List<Inst> program) {
    switch (node.kind()) {
      case SET:
        program.add(new Inst(Op.CHAR, node.set(), 0));
        return;
      case START:
        program.add(new Inst(Op.START, null, 0));
        return;
      case END:
        program.add(new Inst(Op.END, null, 0));
        return;
      case CONCAT:
        for (PatternParser.Node part : node.parts()) {
          emit(part, program);
        }
        return;
      case ALT:
        emitAlt(node.parts(), program);
        return;
      case REPEAT:
        emitRepeat(node.body(), node.min(), node.max(), program);
        return;
      default:
        throw new IllegalStateException();
    }
  }

  private static boolean inItem(PatternParser.Item item, int c) {
    boolean hit = false;
    int[] ranges = item.ranges();
    for (int i = 0; i + 1 < ranges.length && !hit; i += 2) {
      hit = ranges[i] <= c && c <= ranges[i + 1];
    }
    return hit != item.negate();
  }

  private static boolean inSet(PatternParser.CharSet set, int c) {
    boolean any = false;
    for (PatternParser.Item item : set.items()) {
      if (inItem(item, c)) {
        any = true;
        break;
      }
    }
    return any != set.negate();
  }

  /**
   * Does the pattern match somewhere in {@code text}? Each instruction is visited once per
   * position.
   */
  boolean matches(String text) {
    int[] input = text.codePoints().toArray();
    int length = input.length;
    int size = program.length;
    int[] seen = new int[size];
    Arrays.fill(seen, -1);
    int[] stack = new int[2 * size + 2];
    int[] current = new int[size];
    int[] next = new int[size];
    int currentCount = 0;
    int nextCount = 0;
    int[] addResult = new int[1];

    for (int pos = 0; ; pos++) {
      // A fresh thread starts at every position.
      boolean matched = add(current, currentCount, 0, pos, length, seen, stack, addResult);
      currentCount = addResult[0];
      if (matched) {
        return true;
      }
      if (pos >= length) {
        return false;
      }
      int c = input[pos];
      nextCount = 0;
      for (int i = 0; i < currentCount; i++) {
        int pc = current[i];
        Inst inst = program[pc];
        if (inst.op == Op.CHAR && inSet(inst.set, c)) {
          addResult[0] = nextCount;
          boolean hit = add(next, nextCount, pc + 1, pos + 1, length, seen, stack, addResult);
          nextCount = addResult[0];
          if (hit) {
            return true;
          }
        }
      }
      int[] swap = current;
      current = next;
      next = swap;
      currentCount = nextCount;
    }
  }

  /**
   * Adds {@code pc} and everything it reaches without reading to {@code list}; true when that
   * reaches a match. The new count of the list goes to {@code out[0]}.
   */
  private boolean add(
      int[] list, int count, int pc, int pos, int length, int[] seen, int[] stack, int[] out) {
    int n = count;
    int top = 0;
    stack[top++] = pc;
    while (top > 0) {
      int at = stack[--top];
      if (seen[at] == pos) {
        continue;
      }
      seen[at] = pos;
      Inst inst = program[at];
      switch (inst.op) {
        case MATCH:
          out[0] = n;
          return true;
        case CHAR:
          list[n++] = at;
          break;
        case SPLIT:
          stack[top++] = inst.y;
          stack[top++] = inst.x;
          break;
        case START:
          if (pos == 0) {
            stack[top++] = at + 1;
          }
          break;
        case END:
          if (pos == length) {
            stack[top++] = at + 1;
          }
          break;
        default:
          throw new IllegalStateException();
      }
    }
    out[0] = n;
    return false;
  }
}
