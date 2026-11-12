package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.CharSet;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Item;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.PatternParser.Node;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * Patterns for the {@code matches} op: the parsed tree is compiled to a small program and run on a
 * Pike VM over code points. There is no backtracking and no platform pattern engine, so a match
 * costs the program size times the input length whatever the pattern.
 */
final class Regex {
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

  /** A compiled pattern. */
  static final class Program {
    private final Inst[] insts;

    Program(Inst[] insts) {
      this.insts = insts;
    }
  }

  private Regex() {}

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
    // A body of size 0 is empty groups only and matches just the empty string: emit nothing.
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
    switch (node.kind) {
      case SET:
        program.add(new Inst(CHAR, node.set, 0));
        break;
      case START:
        program.add(new Inst(START, null, 0));
        break;
      case END:
        program.add(new Inst(END, null, 0));
        break;
      case CONCAT:
        for (Node part : node.parts) {
          emit(part, program);
        }
        break;
      case ALT:
        emitAlt(node.parts, program);
        break;
      case REPEAT:
        emitRepeat(node.body, node.min, node.max, program);
        break;
      default:
        throw new IllegalStateException("unknown node");
    }
  }

  /** The compiled pattern, or {@code null} when the text is not a valid pattern. */
  static Program compile(String pattern) {
    Node node = PatternParser.parse(PatternParser.codePoints(pattern));
    if (node == null) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(MATCH, null, 0));
    return new Program(program.toArray(new Inst[0]));
  }

  private static boolean inItem(Item item, int c) {
    boolean hit = false;
    for (int i = 0; i + 1 < item.ranges.length && !hit; i += 2) {
      hit = item.ranges[i] <= c && c <= item.ranges[i + 1];
    }
    return hit != item.negate;
  }

  private static boolean inSet(CharSet set, int c) {
    boolean any = false;
    for (Item item : set.items) {
      if (inItem(item, c)) {
        any = true;
        break;
      }
    }
    return any != set.negate;
  }

  /** The threads of one step: the char instructions waiting to read. */
  private static final class Threads {
    final int[] pcs;
    int n;

    Threads(int capacity) {
      this.pcs = new int[capacity];
    }
  }

  /**
   * Does the pattern match somewhere in the text? Each instruction is visited once per position.
   */
  static boolean matches(Program program, String text) {
    Inst[] insts = program.insts;
    int[] input = PatternParser.codePoints(text);
    int length = input.length;
    int[] seen = new int[insts.length];
    Arrays.fill(seen, -1);
    int[] stack = new int[2 * insts.length + 2];
    Threads current = new Threads(insts.length);
    Threads next = new Threads(insts.length);

    for (int pos = 0; ; pos++) {
      if (add(insts, seen, stack, current, 0, pos, length)) {
        return true;
      }
      if (pos >= length) {
        return false;
      }
      int c = input[pos];
      next.n = 0;
      for (int i = 0; i < current.n; i++) {
        int pc = current.pcs[i];
        if (inSet(insts[pc].set, c) && add(insts, seen, stack, next, pc + 1, pos + 1, length)) {
          return true;
        }
      }
      Threads swap = current;
      current = next;
      next = swap;
    }
  }

  /** Adds pc and what it reaches without reading; true when that reaches a match. */
  private static boolean add(
      Inst[] insts, int[] seen, int[] stack, Threads list, int pc, int pos, int length) {
    int top = 0;
    stack[top++] = pc;
    while (top > 0) {
      int at = stack[--top];
      if (at >= insts.length || seen[at] == pos) {
        continue;
      }
      seen[at] = pos;
      Inst inst = insts[at];
      switch (inst.op) {
        case MATCH:
          return true;
        case CHAR:
          list.pcs[list.n++] = at;
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
          throw new IllegalStateException("unknown instruction");
      }
    }
    return false;
  }
}
