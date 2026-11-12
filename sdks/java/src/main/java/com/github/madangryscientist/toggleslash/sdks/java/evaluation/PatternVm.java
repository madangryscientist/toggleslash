package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * A pattern for the {@code matches} op: a hand-written parser to a small program and a Pike VM over
 * code points. There is no backtracking and no library regular expression, so a match costs
 * O(program x input) whatever the pattern. A lone surrogate is a code point of its own.
 */
final class PatternVm {
  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10000;
  private static final int MAX_COUNT = 100;
  private static final int NEWLINE = 0x0A;
  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";

  private static final Item DIGIT = new Item(new int[] {0x30, 0x39}, false);
  private static final Item WORD =
      new Item(new int[] {0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A}, false);
  private static final Item SPACE = new Item(new int[] {0x09, 0x0D, 0x20, 0x20}, false);
  private static final CharSet ANY =
      new CharSet(new Item[] {new Item(new int[] {NEWLINE, NEWLINE}, true)}, false);

  private final Inst[] program;

  private PatternVm(Inst[] program) {
    this.program = program;
  }

  /** Code point ranges as pairs lo, hi, or their complement when negate is set. */
  private record Item(int[] ranges, boolean negate) {
    boolean contains(int c) {
      boolean hit = false;
      for (int i = 0; i < ranges.length && !hit; i += 2) {
        hit = ranges[i] <= c && c <= ranges[i + 1];
      }
      return hit != negate;
    }
  }

  /** A class: any of its items, or none of them when negate is set. */
  private record CharSet(Item[] items, boolean negate) {
    boolean contains(int c) {
      boolean hit = false;
      for (Item item : items) {
        if (item.contains(c)) {
          hit = true;
          break;
        }
      }
      return hit != negate;
    }
  }

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
    final int max;

    /** A max of -1 means no upper bound. */
    RepeatNode(Node body, int min, int max, int size) {
      super(size);
      this.body = body;
      this.min = min;
      this.max = max;
    }
  }

  private enum OpCode {
    CHAR,
    SPLIT,
    JMP,
    START,
    END,
    MATCH
  }

  private static final class Inst {
    final OpCode op;
    final CharSet set;
    int x;
    int y;

    Inst(OpCode op, CharSet set) {
      this.op = op;
      this.set = set;
    }
  }

  /** Thrown on an invalid pattern and caught in {@code Parser.parse}; it never leaves the class. */
  private static final class InvalidException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    InvalidException() {
      super(null, null, false, false);
    }
  }

  /** The code points of a UTF-16 string: a valid pair is one, a lone surrogate is itself. */
  private static int[] codePoints(String text) {
    int[] points = new int[text.length()];
    int n = 0;
    for (int i = 0; i < text.length(); ) {
      int cp = text.codePointAt(i);
      points[n++] = cp;
      i += Character.charCount(cp);
    }
    return java.util.Arrays.copyOf(points, n);
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
   * @return the compiled pattern, or null when the text is not a valid pattern
   */
  static PatternVm compile(String pattern) {
    int[] cps = codePoints(pattern);
    if (cps.length > MAX_LENGTH) {
      return null;
    }
    Node node = new Parser(cps).parse();
    if (node == null || node.size > MAX_SIZE) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(OpCode.MATCH, null));
    return new PatternVm(program.toArray(new Inst[0]));
  }

  private record Escaped(int point, Item item) {}

  private static final class Parser {
    private final int[] cps;
    private int pos;

    Parser(int[] cps) {
      this.cps = cps;
    }

    private int peek(int offset) {
      return pos + offset < cps.length ? cps[pos + offset] : -1;
    }

    private int peek() {
      return peek(0);
    }

    Node parse() {
      try {
        Node node = alternation();
        return pos < cps.length ? null : node;
      } catch (InvalidException e) {
        return null;
      }
    }

    private Node alternation() {
      List<Node> sides = new ArrayList<>();
      sides.add(concat());
      while (peek() == '|') {
        pos++;
        sides.add(concat());
      }
      if (sides.size() == 1) {
        return sides.get(0);
      }
      int sum = 0;
      for (Node side : sides) {
        sum = saturate(sum + side.size);
      }
      return new AltNode(sides, saturate(sum + sides.size() - 1));
    }

    private Node concat() {
      List<Node> parts = new ArrayList<>();
      int sum = 0;
      for (int c = peek(); c != -1 && c != '|' && c != ')'; c = peek()) {
        Node atom = atom();
        int next = peek();
        boolean anchor = atom instanceof StartNode || atom instanceof EndNode;
        boolean quantifier = next == '*' || next == '+' || next == '?' || next == '{';
        Node part = !anchor && quantifier ? quantify(atom) : atom;
        parts.add(part);
        sum = saturate(sum + part.size);
      }
      return new ConcatNode(parts, sum);
    }

    private Node atom() {
      int c = peek();
      pos++;
      switch (c) {
        case '(':
          Node inner = alternation();
          if (peek() != ')') {
            throw new InvalidException();
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
          Item item = escaped.item() != null ? escaped.item() : single(escaped.point());
          return new SetNode(new CharSet(new Item[] {item}, false));
        case '*':
        case '+':
        case '?':
        case '{':
        case '}':
        case ']':
          throw new InvalidException();
        default:
          return new SetNode(new CharSet(new Item[] {single(c)}, false));
      }
    }

    /** After a backslash: one code point, or one of the six shorthand classes. */
    private Escaped escape() {
      int c = peek();
      if (c == -1) {
        throw new InvalidException();
      }
      pos++;
      if (c < 0x80 && ESCAPABLE.indexOf((char) c) >= 0) {
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
          return new Escaped(0, new Item(DIGIT.ranges(), true));
        case 'W':
          return new Escaped(0, new Item(WORD.ranges(), true));
        case 'S':
          return new Escaped(0, new Item(SPACE.ranges(), true));
        default:
          throw new InvalidException();
      }
    }

    /** After an opening bracket: the members up to the closing one. */
    private CharSet charClass() {
      boolean negate = peek() == '^';
      if (negate) {
        pos++;
      }
      List<Item> items = new ArrayList<>();
      for (boolean first = true; ; first = false) {
        int c = peek();
        if (c == -1) {
          throw new InvalidException();
        }
        if (c == ']') {
          if (first) {
            throw new InvalidException();
          }
          pos++;
          return new CharSet(items.toArray(new Item[0]), negate);
        }
        Escaped from = classAtom(first);
        if (peek() == '-' && peek(1) != -1 && peek(1) != ']') {
          pos++;
          Escaped to = classAtom(false);
          if (from.item() != null || to.item() != null || from.point() > to.point()) {
            throw new InvalidException();
          }
          items.add(new Item(new int[] {from.point(), to.point()}, false));
        } else {
          items.add(from.item() != null ? from.item() : single(from.point()));
        }
      }
    }

    private Escaped classAtom(boolean first) {
      int c = peek();
      if (c == -1 || c == '[') {
        throw new InvalidException();
      }
      pos++;
      if (c == '\\') {
        return escape();
      }
      // An unescaped hyphen is a literal only as the first member or directly before the bracket.
      if (c == '-' && !first && peek() != ']') {
        throw new InvalidException();
      }
      return new Escaped(c, null);
    }

    private Node quantify(Node body) {
      int c = peek();
      pos++;
      if (c != '{') {
        return new RepeatNode(body, c == '+' ? 1 : 0, c == '?' ? 1 : -1, saturate(body.size + 1));
      }
      int min = count();
      int max = min;
      int times = min;
      if (peek() == ',') {
        pos++;
        if (peek() == '}') {
          max = -1;
          times = min + 1;
        } else {
          max = count();
          times = max;
        }
      }
      if (peek() != '}' || (max >= 0 && min > max)) {
        throw new InvalidException();
      }
      pos++;
      return new RepeatNode(body, min, max, Math.max(1, saturate(times * body.size)));
    }

    /** ASCII digits with no leading zero, at most 100. */
    private int count() {
      int c = peek();
      if (!isDigit(c)) {
        throw new InvalidException();
      }
      pos++;
      if (c == '0') {
        if (isDigit(peek())) {
          throw new InvalidException();
        }
        return 0;
      }
      int value = c - '0';
      for (int d = peek(); isDigit(d); d = peek()) {
        value = value * 10 + (d - '0');
        if (value > MAX_COUNT) {
          throw new InvalidException();
        }
        pos++;
      }
      return value;
    }
  }

  private static void emit(Node node, List<Inst> program) {
    if (node instanceof SetNode set) {
      program.add(new Inst(OpCode.CHAR, set.set));
    } else if (node instanceof StartNode) {
      program.add(new Inst(OpCode.START, null));
    } else if (node instanceof EndNode) {
      program.add(new Inst(OpCode.END, null));
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
        Inst split = new Inst(OpCode.SPLIT, null);
        split.x = program.size() + 1;
        program.add(split);
        emit(alt.sides.get(i), program);
        Inst jump = new Inst(OpCode.JMP, null);
        jumps.add(jump);
        program.add(jump);
        split.y = program.size();
      }
      for (Inst jump : jumps) {
        jump.x = program.size();
      }
    } else if (node instanceof RepeatNode repeat) {
      emitRepeat(repeat, program);
    }
  }

  private static void emitRepeat(RepeatNode repeat, List<Inst> program) {
    // A body of size 0 is empty groups only and matches just the empty string, so repeating it
    // emits nothing.
    if (repeat.body.size == 0) {
      return;
    }
    for (int i = 0; i < repeat.min; i++) {
      emit(repeat.body, program);
    }
    if (repeat.max < 0) {
      int loop = program.size();
      Inst star = new Inst(OpCode.SPLIT, null);
      star.x = loop + 1;
      program.add(star);
      emit(repeat.body, program);
      Inst back = new Inst(OpCode.JMP, null);
      back.x = loop;
      program.add(back);
      star.y = program.size();
      return;
    }
    List<Inst> splits = new ArrayList<>();
    for (int i = repeat.min; i < repeat.max; i++) {
      Inst split = new Inst(OpCode.SPLIT, null);
      split.x = program.size() + 1;
      splits.add(split);
      program.add(split);
      emit(repeat.body, program);
    }
    for (Inst split : splits) {
      split.y = program.size();
    }
  }

  /** Mutable state for one match: lists of program counters and the visit marks. */
  private final class Run {
    final int[] seen = new int[program.length];
    final int[] stack = new int[program.length * 2 + 2];
    int[] current = new int[program.length];
    int currentSize;
    int[] next = new int[program.length];
    int nextSize;
    final int length;

    Run(int length) {
      this.length = length;
      java.util.Arrays.fill(seen, -1);
    }

    /** Adds pc and everything it reaches without reading; true when that reaches a match. */
    boolean add(boolean toNext, int pc, int pos) {
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
          case CHAR:
            if (toNext) {
              next[nextSize++] = at;
            } else {
              current[currentSize++] = at;
            }
            break;
          case JMP:
            stack[top++] = inst.x;
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
          case MATCH:
            return true;
          default:
            break;
        }
      }
      return false;
    }
  }

  /**
   * Does the pattern match somewhere in the text? Each instruction is visited once per position.
   *
   * @param text the text
   * @return whether it matches
   */
  boolean isMatch(String text) {
    int[] input = codePoints(text);
    Run run = new Run(input.length);
    for (int pos = 0; ; pos++) {
      if (run.add(false, 0, pos)) {
        return true;
      }
      if (pos == input.length) {
        return false;
      }
      run.nextSize = 0;
      for (int i = 0; i < run.currentSize; i++) {
        int pc = run.current[i];
        if (program[pc].set.contains(input[pos]) && run.add(true, pc + 1, pos + 1)) {
          return true;
        }
      }
      int[] swap = run.current;
      run.current = run.next;
      run.next = swap;
      run.currentSize = run.nextSize;
    }
  }
}
