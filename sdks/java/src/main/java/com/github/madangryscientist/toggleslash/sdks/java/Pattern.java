package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.List;

/**
 * A pattern for the {@code matches} op: a hand-written parser to a small program, run as a Pike VM
 * over code points. There is no backtracking and no {@code java.util.regex}, so a match costs
 * O(program x input) whatever the pattern. A lone surrogate is a code point of its own here.
 */
final class Pattern {
  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10000;
  private static final int MAX_COUNT = 100;
  private static final int NEWLINE = 0x0A;

  private static final int OP_CHAR = 0;
  private static final int OP_SPLIT = 1;
  private static final int OP_JMP = 2;
  private static final int OP_START = 3;
  private static final int OP_END = 4;
  private static final int OP_MATCH = 5;

  private final int[] ops;
  private final int[] xs;
  private final int[] ys;
  private final CharSet[] sets;

  private Pattern(List<Inst> program) {
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

  /** Code point ranges as pairs (low, high, inclusive), or their complement when negated. */
  private record Item(int[] ranges, boolean negate) {
    boolean contains(int c) {
      boolean hit = false;
      for (int i = 0; i < ranges.length && !hit; i += 2) {
        hit = ranges[i] <= c && c <= ranges[i + 1];
      }
      return hit != negate;
    }

    Item complement() {
      return new Item(ranges, !negate);
    }
  }

  /** A class: any of its items, or none of them when negated. */
  private record CharSet(Item[] items, boolean negate) {
    boolean contains(int c) {
      boolean any = false;
      for (Item item : items) {
        if (item.contains(c)) {
          any = true;
          break;
        }
      }
      return any != negate;
    }
  }

  private static final Item DIGIT = new Item(new int[] {0x30, 0x39}, false);
  private static final Item WORD =
      new Item(new int[] {0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A}, false);
  private static final Item SPACE = new Item(new int[] {0x09, 0x0D, 0x20, 0x20}, false);
  private static final CharSet ANY =
      new CharSet(new Item[] {new Item(new int[] {NEWLINE, NEWLINE}, true)}, false);

  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";

  private abstract static class Node {
    final int size;

    Node(int size) {
      this.size = size;
    }
  }

  private static final class SetNode extends Node {
    final CharSet set;

    SetNode(CharSet set) {
      super(1);
      this.set = set;
    }
  }

  private static final class StartNode extends Node {
    StartNode() {
      super(1);
    }
  }

  private static final class EndNode extends Node {
    EndNode() {
      super(1);
    }
  }

  private static final class ConcatNode extends Node {
    final List<Node> parts;

    ConcatNode(List<Node> parts, int size) {
      super(size);
      this.parts = parts;
    }
  }

  private static final class AltNode extends Node {
    final List<Node> sides;

    AltNode(List<Node> sides, int size) {
      super(size);
      this.sides = sides;
    }
  }

  private static final class RepeatNode extends Node {
    final Node body;
    final int min;
    final int max; // -1 for no upper bound

    RepeatNode(Node body, int min, int max, int size) {
      super(size);
      this.body = body;
      this.min = min;
      this.max = max;
    }
  }

  private static final class Inst {
    final int op;
    final CharSet set;
    int x;
    int y;

    Inst(int op, CharSet set) {
      this.op = op;
      this.set = set;
    }
  }

  /** Thrown on an invalid pattern and caught in {@link #compile}; it never leaves this class. */
  private static final class Invalid extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Invalid() {
      super(null, null, false, false);
    }
  }

  /** The code points of a UTF-16 string: a valid pair is one, a lone surrogate is itself. */
  private static int[] codePoints(String text) {
    int[] points = new int[text.length()];
    int count = 0;
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (Character.isHighSurrogate(c)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        points[count++] = Character.toCodePoint(c, text.charAt(i + 1));
        i++;
      } else {
        points[count++] = c;
      }
    }
    return java.util.Arrays.copyOf(points, count);
  }

  private static int saturate(int size) {
    return Math.min(size, MAX_SIZE + 1);
  }

  private static boolean isDigit(int c) {
    return c >= '0' && c <= '9';
  }

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  /**
   * Compiles a pattern.
   *
   * @param pattern the pattern text
   * @return the compiled pattern, or null when {@code pattern} is not a valid pattern
   */
  static Pattern compile(String pattern) {
    int[] cps = codePoints(pattern);
    if (cps.length > MAX_LENGTH) {
      return null;
    }
    Node node;
    try {
      node = new Parser(cps).parse();
    } catch (Invalid e) {
      return null;
    }
    if (node.size > MAX_SIZE) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(OP_MATCH, null));
    return new Pattern(program);
  }

  private static final class Parser {
    private final int[] cps;
    private int pos;

    Parser(int[] cps) {
      this.cps = cps;
    }

    private int peek(int offset) {
      return pos + offset < cps.length ? cps[pos + offset] : -1;
    }

    Node parse() {
      Node node = alternation();
      if (pos < cps.length) {
        throw new Invalid(); // an unmatched ')'
      }
      return node;
    }

    private Node alternation() {
      List<Node> sides = new ArrayList<>();
      sides.add(concat());
      while (peek(0) == '|') {
        pos++;
        sides.add(concat());
      }
      if (sides.size() == 1) {
        return sides.get(0);
      }
      int total = sides.size() - 1;
      for (Node side : sides) {
        total = saturate(total + side.size);
      }
      return new AltNode(sides, total);
    }

    private ConcatNode concat() {
      List<Node> parts = new ArrayList<>();
      int total = 0;
      for (int c = peek(0); c != -1 && c != '|' && c != ')'; c = peek(0)) {
        Node atom = atom();
        int next = peek(0);
        boolean quantified =
            !(atom instanceof StartNode)
                && !(atom instanceof EndNode)
                && (next == '*' || next == '+' || next == '?' || next == '{');
        Node part = quantified ? quantify(atom) : atom;
        parts.add(part);
        total = saturate(total + part.size);
      }
      return new ConcatNode(parts, total);
    }

    private Node atom() {
      int c = peek(0);
      pos++;
      switch (c) {
        case '(':
          Node inner = alternation();
          if (peek(0) != ')') {
            throw new Invalid();
          }
          pos++;
          return inner;
        case '[':
          return new SetNode(charClass());
        case '.':
          return new SetNode(ANY);
        case '^':
          return new StartNode();
        case '$':
          return new EndNode();
        case '\\':
          Escaped escaped = escape();
          Item item = escaped.item != null ? escaped.item : single(escaped.point);
          return new SetNode(new CharSet(new Item[] {item}, false));
        case '*':
        case '+':
        case '?':
        case '{':
        case '}':
        case ']':
          throw new Invalid();
        default:
          return new SetNode(new CharSet(new Item[] {single(c)}, false));
      }
    }

    private record Escaped(int point, Item item) {}

    /** After a backslash: one code point, or one of the six shorthand classes. */
    private Escaped escape() {
      int c = peek(0);
      if (c == -1) {
        throw new Invalid();
      }
      pos++;
      if (c < 0x80 && ESCAPABLE.indexOf(c) >= 0) {
        return new Escaped(c, null);
      }
      switch (c) {
        case 'n':
          return new Escaped(0x0A, null);
        case 't':
          return new Escaped(0x09, null);
        case 'r':
          return new Escaped(0x0D, null);
        case 'f':
          return new Escaped(0x0C, null);
        case 'v':
          return new Escaped(0x0B, null);
        case 'd':
          return new Escaped(0, DIGIT);
        case 'w':
          return new Escaped(0, WORD);
        case 's':
          return new Escaped(0, SPACE);
        case 'D':
          return new Escaped(0, DIGIT.complement());
        case 'W':
          return new Escaped(0, WORD.complement());
        case 'S':
          return new Escaped(0, SPACE.complement());
        default:
          throw new Invalid();
      }
    }

    /** After {@code [}: the members up to {@code ]}. */
    private CharSet charClass() {
      boolean negate = peek(0) == '^';
      if (negate) {
        pos++;
      }
      List<Item> items = new ArrayList<>();
      for (boolean first = true; ; first = false) {
        int c = peek(0);
        if (c == -1) {
          throw new Invalid();
        }
        if (c == ']') {
          if (first) {
            throw new Invalid();
          }
          pos++;
          return new CharSet(items.toArray(new Item[0]), negate);
        }
        Escaped from = classAtom(first);
        if (peek(0) == '-' && peek(1) != -1 && peek(1) != ']') {
          pos++;
          Escaped to = classAtom(false);
          if (from.item != null || to.item != null || from.point > to.point) {
            throw new Invalid();
          }
          items.add(new Item(new int[] {from.point, to.point}, false));
        } else {
          items.add(from.item != null ? from.item : single(from.point));
        }
      }
    }

    private Escaped classAtom(boolean first) {
      int c = peek(0);
      if (c == -1 || c == '[') {
        throw new Invalid();
      }
      pos++;
      if (c == '\\') {
        return escape();
      }
      // An unescaped '-' is a literal only as the first member or directly before ']'.
      if (c == '-' && !first && peek(0) != ']') {
        throw new Invalid();
      }
      return new Escaped(c, null);
    }

    private Node quantify(Node body) {
      int c = peek(0);
      pos++;
      if (c != '{') {
        return new RepeatNode(body, c == '+' ? 1 : 0, c == '?' ? 1 : -1, saturate(body.size + 1));
      }
      int min = count();
      int max = min;
      int times = min;
      if (peek(0) == ',') {
        pos++;
        if (peek(0) == '}') {
          max = -1;
          times = min + 1;
        } else {
          max = count();
          times = max;
        }
      }
      if (peek(0) != '}' || (max != -1 && min > max)) {
        throw new Invalid();
      }
      pos++;
      return new RepeatNode(body, min, max, Math.max(1, saturate(times * body.size)));
    }

    /** ASCII digits with no leading zero, at most 100. */
    private int count() {
      int c = peek(0);
      if (!isDigit(c)) {
        throw new Invalid();
      }
      pos++;
      if (c == '0') {
        if (isDigit(peek(0))) {
          throw new Invalid();
        }
        return 0;
      }
      int value = c - '0';
      for (int d = peek(0); isDigit(d); d = peek(0)) {
        value = value * 10 + (d - '0');
        if (value > MAX_COUNT) {
          throw new Invalid();
        }
        pos++;
      }
      return value;
    }
  }

  private static void emit(Node node, List<Inst> program) {
    if (node instanceof SetNode set) {
      program.add(new Inst(OP_CHAR, set.set));
    } else if (node instanceof StartNode) {
      program.add(new Inst(OP_START, null));
    } else if (node instanceof EndNode) {
      program.add(new Inst(OP_END, null));
    } else if (node instanceof ConcatNode concat) {
      for (Node part : concat.parts) {
        emit(part, program);
      }
    } else if (node instanceof AltNode alt) {
      List<Inst> jumps = new ArrayList<>();
      for (int i = 0; i < alt.sides.size(); i++) {
        if (i == alt.sides.size() - 1) {
          emit(alt.sides.get(i), program);
          break;
        }
        Inst split = new Inst(OP_SPLIT, null);
        split.x = program.size() + 1;
        program.add(split);
        emit(alt.sides.get(i), program);
        Inst jump = new Inst(OP_JMP, null);
        jumps.add(jump);
        program.add(jump);
        split.y = program.size();
      }
      for (Inst jump : jumps) {
        jump.x = program.size();
      }
    } else if (node instanceof RepeatNode repeat) {
      // A body of size 0 is empty groups only and matches just the empty string, so repeating it
      // emits
      // nothing; its copies would cost instructions that the size does not count.
      if (repeat.body.size == 0) {
        return;
      }
      for (int i = 0; i < repeat.min; i++) {
        emit(repeat.body, program);
      }
      if (repeat.max == -1) {
        int loop = program.size();
        Inst star = new Inst(OP_SPLIT, null);
        star.x = loop + 1;
        program.add(star);
        emit(repeat.body, program);
        Inst back = new Inst(OP_JMP, null);
        back.x = loop;
        program.add(back);
        star.y = program.size();
        return;
      }
      List<Inst> splits = new ArrayList<>();
      for (int i = repeat.min; i < repeat.max; i++) {
        Inst split = new Inst(OP_SPLIT, null);
        split.x = program.size() + 1;
        splits.add(split);
        program.add(split);
        emit(repeat.body, program);
      }
      for (Inst split : splits) {
        split.y = program.size();
      }
    }
  }

  /**
   * Does the pattern match somewhere in {@code text}? Each instruction is visited once per
   * position.
   *
   * @param text the candidate
   * @return true if it matches
   */
  boolean isMatch(String text) {
    int[] input = codePoints(text);
    int length = input.length;
    int n = ops.length;
    int[] seen = new int[n];
    java.util.Arrays.fill(seen, -1);
    int[] stack = new int[n * 2 + 2];
    int[] current = new int[n];
    int[] next = new int[n];
    int currentCount = 0;
    for (int pos = 0; ; pos++) {
      int nextCount = 0;
      // Start a thread at this position, then step the ones carried from the last.
      int[] added = addThread(current, currentCount, 0, pos, length, seen, stack);
      if (added[1] == 1) {
        return true;
      }
      currentCount = added[0];
      if (pos == length) {
        return false;
      }
      for (int i = 0; i < currentCount; i++) {
        int pc = current[i];
        if (sets[pc].contains(input[pos])) {
          int[] stepped = addThread(next, nextCount, pc + 1, pos + 1, length, seen, stack);
          if (stepped[1] == 1) {
            return true;
          }
          nextCount = stepped[0];
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
   * @return {new count, 1 when it reached a match}
   */
  private int[] addThread(
      int[] list, int count, int pc, int pos, int length, int[] seen, int[] stack) {
    int sp = 0;
    stack[sp++] = pc;
    while (sp > 0) {
      int at = stack[--sp];
      if (seen[at] == pos) {
        continue;
      }
      seen[at] = pos;
      switch (ops[at]) {
        case OP_CHAR:
          list[count++] = at;
          break;
        case OP_JMP:
          stack[sp++] = xs[at];
          break;
        case OP_SPLIT:
          stack[sp++] = ys[at];
          stack[sp++] = xs[at];
          break;
        case OP_START:
          if (pos == 0) {
            stack[sp++] = at + 1;
          }
          break;
        case OP_END:
          if (pos == length) {
            stack[sp++] = at + 1;
          }
          break;
        default:
          return new int[] {count, 1};
      }
    }
    return new int[] {count, 0};
  }
}
