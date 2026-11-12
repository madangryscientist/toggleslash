package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Alt;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.AnchorNode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.CharSet;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Concat;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Item;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Node;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Repeat;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.SetNode;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * A compiled {@code matches} pattern, run as a Pike VM over code points: no backtracking and no
 * {@code java.util.regex}, so a match costs O(program x input) whatever the pattern. A program is
 * immutable and safe to share between threads.
 */
final class PatternProgram {

  private static final int CHAR = 0;
  private static final int SPLIT = 1;
  private static final int START = 2;
  private static final int END = 3;
  private static final int MATCH = 4;

  private final int[] ops;
  private final int[] xs;
  private final int[] ys;
  private final CharSet[] sets;

  private PatternProgram(List<Inst> program) {
    int n = program.size();
    ops = new int[n];
    xs = new int[n];
    ys = new int[n];
    sets = new CharSet[n];
    for (int i = 0; i < n; i++) {
      Inst inst = program.get(i);
      ops[i] = inst.op;
      xs[i] = inst.x;
      ys[i] = inst.y;
      sets[i] = inst.set;
    }
  }

  private static final class Inst {
    final int op;
    int x;
    int y;
    final CharSet set;

    Inst(int op, int x, int y, CharSet set) {
      this.op = op;
      this.x = x;
      this.y = y;
      this.set = set;
    }
  }

  /** The compiled pattern, or null when {@code pattern} is not a valid pattern. */
  static PatternProgram compile(String pattern) {
    Node node = PatternParser.parse(pattern);
    if (node == null) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(MATCH, 0, 0, null));
    return new PatternProgram(program);
  }

  private static Inst split(List<Inst> program, int x) {
    Inst inst = new Inst(SPLIT, x, x, null);
    program.add(inst);
    return inst;
  }

  private static void emit(Node node, List<Inst> program) {
    if (node instanceof SetNode set) {
      program.add(new Inst(CHAR, 0, 0, set.set()));
    } else if (node instanceof AnchorNode anchor) {
      program.add(new Inst(anchor.atStart() ? START : END, 0, 0, null));
    } else if (node instanceof Concat concat) {
      for (Node part : concat.parts()) {
        emit(part, program);
      }
    } else if (node instanceof Alt alt) {
      emitAlt(alt.sides(), program);
    } else if (node instanceof Repeat repeat) {
      emitRepeat(repeat.body(), repeat.min(), repeat.max(), program);
    }
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

  private static boolean inItem(Item item, int c) {
    int[] ranges = item.ranges();
    boolean hit = false;
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
    return new Run(text.codePoints().toArray()).run();
  }

  /** The state of one match: the thread lists and the visit marks. */
  private final class Run {
    private final int[] input;
    private final int[] seen;
    private final int[] stack;
    private int stackSize;
    private int[] current;
    private int currentSize;
    private int[] next;
    private int nextSize;

    Run(int[] input) {
      this.input = input;
      int n = ops.length;
      seen = new int[n];
      Arrays.fill(seen, -1);
      stack = new int[2 * n + 2];
      current = new int[n];
      next = new int[n];
    }

    /**
     * Adds {@code pc} and everything it reaches without reading; true when that reaches a match.
     */
    private boolean add(boolean toNext, int pc, int pos) {
      stackSize = 0;
      stack[stackSize++] = pc;
      while (stackSize > 0) {
        int at = stack[--stackSize];
        if (seen[at] == pos) {
          continue;
        }
        seen[at] = pos;
        switch (ops[at]) {
          case MATCH:
            return true;
          case CHAR:
            if (toNext) {
              next[nextSize++] = at;
            } else {
              current[currentSize++] = at;
            }
            break;
          case SPLIT:
            stack[stackSize++] = ys[at];
            stack[stackSize++] = xs[at];
            break;
          case START:
            if (pos == 0) {
              stack[stackSize++] = at + 1;
            }
            break;
          default:
            if (pos == input.length) {
              stack[stackSize++] = at + 1;
            }
            break;
        }
      }
      return false;
    }

    boolean run() {
      for (int pos = 0; ; pos++) {
        if (add(false, 0, pos)) {
          return true;
        }
        if (pos >= input.length) {
          return false;
        }
        int c = input[pos];
        nextSize = 0;
        for (int i = 0; i < currentSize; i++) {
          int pc = current[i];
          if (inSet(sets[pc], c) && add(true, pc + 1, pos + 1)) {
            return true;
          }
        }
        int[] swap = current;
        current = next;
        next = swap;
        currentSize = nextSize;
      }
    }
  }
}
