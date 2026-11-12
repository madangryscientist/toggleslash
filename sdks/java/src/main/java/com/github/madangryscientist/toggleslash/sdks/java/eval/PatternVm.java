package com.github.madangryscientist.toggleslash.sdks.java.eval;

import com.github.madangryscientist.toggleslash.sdks.java.eval.PatternParser.CharSet;
import com.github.madangryscientist.toggleslash.sdks.java.eval.PatternParser.Item;
import com.github.madangryscientist.toggleslash.sdks.java.eval.PatternParser.Node;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * A compiled {@code matches} pattern and the Pike VM that runs it over code points: no backtracking
 * and no platform regular expression, so a match costs O(program x input) whatever the pattern.
 */
final class PatternVm {
  private static final int CHAR = 0;
  private static final int SPLIT = 1;
  private static final int START = 2;
  private static final int END = 3;
  private static final int MATCH = 4;

  private static final class Inst {
    final int op;
    final CharSet set;
    int x;
    int y;

    Inst(int op, CharSet set, int x) {
      this.op = op;
      this.set = set;
      this.x = x;
      this.y = x;
    }
  }

  private final Inst[] program;

  private PatternVm(List<Inst> program) {
    this.program = program.toArray(new Inst[0]);
  }

  /**
   * Compiles a pattern.
   *
   * @param pattern the pattern text
   * @return the program, or null when {@code pattern} is not a valid pattern
   */
  static PatternVm compile(String pattern) {
    Node node = PatternParser.parse(pattern);
    if (node == null) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(MATCH, null, 0));
    return new PatternVm(program);
  }

  private static Inst split(List<Inst> program, int x) {
    Inst inst = new Inst(SPLIT, null, x);
    program.add(inst);
    return inst;
  }

  private static void emitAlt(List<Node> sides, List<Inst> program) {
    List<Inst> jumps = new ArrayList<>();
    int last = sides.size() - 1;
    for (int i = 0; i < sides.size(); i++) {
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

  private static void emitRepeat(Node node, List<Inst> program) {
    Node body = node.body;
    // A body of size 0 is empty groups only and matches just the empty string, so repeating it
    // emits nothing.
    if (body.size == 0) {
      return;
    }
    for (int i = 0; i < node.min; i++) {
      emit(body, program);
    }
    if (node.max < 0) {
      int top = program.size();
      Inst loop = split(program, top + 1);
      emit(body, program);
      split(program, top);
      loop.y = program.size();
      return;
    }
    List<Inst> optional = new ArrayList<>();
    for (int i = node.min; i < node.max; i++) {
      optional.add(split(program, program.size() + 1));
      emit(body, program);
    }
    for (Inst choice : optional) {
      choice.y = program.size();
    }
  }

  private static void emit(Node node, List<Inst> program) {
    switch (node.kind) {
      case Node.SET:
        program.add(new Inst(CHAR, node.set, 0));
        return;
      case Node.START:
        program.add(new Inst(START, null, 0));
        return;
      case Node.END_ANCHOR:
        program.add(new Inst(END, null, 0));
        return;
      case Node.CONCAT:
        for (Node part : node.parts) {
          emit(part, program);
        }
        return;
      case Node.ALT:
        emitAlt(node.parts, program);
        return;
      default:
        emitRepeat(node, program);
    }
  }

  private static boolean inItem(Item item, int c) {
    boolean hit = false;
    int[] ranges = item.ranges();
    for (int i = 0; i + 1 < ranges.length && !hit; i += 2) {
      hit = ranges[i] <= c && c <= ranges[i + 1];
    }
    return hit != item.negate();
  }

  private static boolean inSet(CharSet set, int c) {
    boolean any = false;
    for (Item item : set.items()) {
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
   *
   * @param text the candidate
   * @return whether it matches
   */
  boolean matches(String text) {
    int[] input = text.codePoints().toArray();
    int length = input.length;
    int[] seen = new int[program.length];
    Arrays.fill(seen, -1);
    int[] stack = new int[2 * program.length + 2];
    int[] current = new int[program.length];
    int[] next = new int[program.length];
    int currentCount = 0;
    int nextCount;
    for (int pos = 0; ; pos++) {
      currentCount = add(current, currentCount, 0, pos, length, seen, stack);
      if (currentCount < 0) {
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
        if (inst.op == CHAR && inSet(inst.set, c)) {
          nextCount = add(next, nextCount, pc + 1, pos + 1, length, seen, stack);
          if (nextCount < 0) {
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
   * Adds {@code pc} and everything it reaches without reading to {@code list}.
   *
   * @return the new count, or -1 when that reaches a match
   */
  private int add(int[] list, int count, int pc, int pos, int length, int[] seen, int[] stack) {
    int top = 0;
    stack[top++] = pc;
    int n = count;
    while (top > 0) {
      int at = stack[--top];
      if (seen[at] == pos) {
        continue;
      }
      seen[at] = pos;
      Inst inst = program[at];
      switch (inst.op) {
        case MATCH:
          return -1;
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
        default:
          if (pos == length) {
            stack[top++] = at + 1;
          }
      }
    }
    return n;
  }
}
