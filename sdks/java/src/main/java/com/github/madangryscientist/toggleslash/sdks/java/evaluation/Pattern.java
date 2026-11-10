package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.AltNode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.AnchorNode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.CharSet;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.ConcatNode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Item;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Node;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.RepeatNode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.SetNode;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * Patterns for the {@code matches} op, to the "Patterns" section of {@code docs/evaluation.md}.
 * {@link PatternParser} reads a pattern into a tree; this compiles the tree to a small program and
 * runs it on a Pike VM over code points: no backtracking and no platform regular expression, so a
 * match costs O(program x input) whatever the pattern.
 */
final class Pattern {
  private static final int CHAR = 0;
  private static final int SPLIT = 1;
  private static final int START = 2;
  private static final int END = 3;
  private static final int MATCH = 4;

  /** One instruction. A jump is a split whose two targets are the same. */
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

  /** The compiled pattern, or null when {@code pattern} is not a valid pattern. */
  static Pattern compile(String pattern) {
    Node node = PatternParser.parse(PatternParser.codePoints(pattern));
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

  private static void emitRepeat(Node body, int min, int max, List<Inst> program) {
    // A body of size 0 is empty groups only and matches just the empty string, so repeating it
    // emits nothing. Its copies would otherwise cost instructions that the size does not count.
    if (body.size == 0) {
      return;
    }
    for (int i = 0; i < min; i++) {
      emit(body, program);
    }
    if (max < 0) {
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
    if (node instanceof SetNode set) {
      program.add(new Inst(CHAR, set.set, 0));
    } else if (node instanceof AnchorNode anchor) {
      program.add(new Inst(anchor.start ? START : END, null, 0));
    } else if (node instanceof ConcatNode concat) {
      for (Node part : concat.parts) {
        emit(part, program);
      }
    } else if (node instanceof AltNode alt) {
      emitAlt(alt.sides, program);
    } else if (node instanceof RepeatNode repeat) {
      emitRepeat(repeat.body, repeat.min, repeat.max, program);
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
   */
  boolean matches(String text) {
    int[] input = PatternParser.codePoints(text);
    int length = input.length;
    int[] seen = new int[program.length];
    Arrays.fill(seen, -1);
    int[] stack = new int[program.length * 2 + 2];
    int[] current = new int[program.length];
    int[] next = new int[program.length];
    int currentSize = 0;
    int nextSize;

    for (int pos = 0; ; pos++) {
      Adder adder = new Adder(seen, stack, length);
      currentSize = adder.add(current, currentSize, 0, pos);
      if (currentSize < 0) {
        return true;
      }
      if (pos >= length) {
        return false;
      }
      int c = input[pos];
      nextSize = 0;
      adder = new Adder(seen, stack, length);
      for (int i = 0; i < currentSize; i++) {
        Inst inst = program[current[i]];
        if (inst.op == CHAR && inSet(inst.set, c)) {
          nextSize = adder.add(next, nextSize, current[i] + 1, pos + 1);
          if (nextSize < 0) {
            return true;
          }
        }
      }
      int[] swap = current;
      current = next;
      next = swap;
      currentSize = nextSize;
    }
  }

  /** Follows the instructions that read nothing, from a start point, into a thread list. */
  private final class Adder {
    private final int[] seen;
    private final int[] stack;
    private final int length;

    Adder(int[] seen, int[] stack, int length) {
      this.seen = seen;
      this.stack = stack;
      this.length = length;
    }

    /**
     * Adds {@code pc} and everything it reaches without reading to {@code list}. Returns the new
     * size of the list, or -1 when that reaches a match.
     */
    int add(int[] list, int size, int pc, int pos) {
      int top = 0;
      int count = size;
      stack[top++] = pc;
      while (top > 0) {
        int at = stack[--top];
        if (at >= program.length) {
          continue;
        }
        Inst inst = program[at];
        if (seen[at] == pos) {
          continue;
        }
        seen[at] = pos;
        switch (inst.op) {
          case MATCH:
            return -1;
          case CHAR:
            list[count++] = at;
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
            break;
        }
      }
      return count;
    }
  }
}
