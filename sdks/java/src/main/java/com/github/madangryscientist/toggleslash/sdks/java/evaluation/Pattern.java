package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.CharSet;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Item;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Node;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * A compiled {@code matches} pattern, to the "Patterns" section of the spec. {@link PatternParser}
 * reads a pattern into a tree; this compiles the tree to a small program and runs it on a Pike VM
 * over code points: no backtracking and no {@code java.util.regex}, so a match costs O(program
 * times input) whatever the pattern.
 */
final class Pattern {
  private static final int CHAR = 0;
  private static final int SPLIT = 1;
  private static final int START = 2;
  private static final int END = 3;
  private static final int MATCH = 4;

  /** One instruction; {@code x} and {@code y} are the two targets of a split. */
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

  private Pattern(Inst[] program) {
    this.program = program;
  }

  /** The compiled pattern, or null when the text is not a valid pattern. */
  static Pattern compile(String pattern) {
    Node node = PatternParser.parse(CodePoints.of(pattern));
    if (node == null) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(MATCH, null, 0));
    return new Pattern(program.toArray(new Inst[0]));
  }

  private static Inst split(List<Inst> program, int x) {
    Inst inst = new Inst(SPLIT, null, x);
    program.add(inst);
    return inst;
  }

  private static void emitAlt(List<Node> sides, List<Inst> program) {
    List<Inst> jumps = new ArrayList<>();
    int last = sides.size() - 1;
    for (int i = 0; i <= last; i++) {
      Inst choice = i < last ? split(program, program.size() + 1) : null;
      emit(sides.get(i), program);
      if (choice != null) {
        jumps.add(split(program, 0));
        choice.y = program.size();
      }
    }
    for (Inst jump : jumps) {
      jump.x = program.size();
      jump.y = program.size();
    }
  }

  private static void emitRepeat(Node body, int min, int max, List<Inst> program) {
    // A body of size 0 is empty groups only and matches just the empty string, so repeating it
    // emits nothing.
    if (body.size == 0) {
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

  private static void emit(Node node, List<Inst> program) {
    switch (node.kind) {
      case SET:
        program.add(new Inst(CHAR, node.set, 0));
        return;
      case START:
        program.add(new Inst(START, null, 0));
        return;
      case END:
        program.add(new Inst(END, null, 0));
        return;
      case CONCAT:
        for (Node part : node.parts) {
          emit(part, program);
        }
        return;
      case ALT:
        emitAlt(node.parts, program);
        return;
      case REPEAT:
        emitRepeat(node.body, node.min, node.max, program);
        return;
      default:
        throw new IllegalStateException("unknown node kind");
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

  /** The state of one run: each instruction is visited once per position. */
  private final class Run {
    final int[] seen = new int[program.length];
    final int[] stack = new int[2 * program.length + 2];
    final int length;

    Run(int length) {
      this.length = length;
      Arrays.fill(seen, -1);
    }

    /**
     * Adds the instruction and all it reaches without reading; the new count, or -1 on reaching a
     * match.
     */
    int add(int[] list, int count, int pc, int pos) {
      int sp = 0;
      stack[sp++] = pc;
      int n = count;
      while (sp > 0) {
        int at = stack[--sp];
        Inst inst = program[at];
        if (seen[at] == pos) {
          continue;
        }
        seen[at] = pos;
        switch (inst.op) {
          case MATCH:
            return -1;
          case CHAR:
            list[n++] = at;
            break;
          case SPLIT:
            stack[sp++] = inst.y;
            stack[sp++] = inst.x;
            break;
          case START:
            if (pos == 0) {
              stack[sp++] = at + 1;
            }
            break;
          default:
            if (pos == length) {
              stack[sp++] = at + 1;
            }
            break;
        }
      }
      return n;
    }
  }

  /** Whether the pattern matches somewhere in the text. */
  boolean matches(String text) {
    int[] input = CodePoints.of(text);
    Run run = new Run(input.length);
    int[] current = new int[program.length];
    int[] next = new int[program.length];
    int currentCount = 0;
    for (int pos = 0; ; pos++) {
      currentCount = run.add(current, currentCount, 0, pos);
      if (currentCount < 0) {
        return true;
      }
      if (pos == input.length) {
        return false;
      }
      int c = input[pos];
      int nextCount = 0;
      for (int i = 0; i < currentCount; i++) {
        int pc = current[i];
        if (inSet(program[pc].set, c)) {
          nextCount = run.add(next, nextCount, pc + 1, pos + 1);
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
}
