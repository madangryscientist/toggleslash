package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * Patterns for the {@code matches} op, to the "Patterns" section of {@code docs/evaluation.md}. A
 * pattern is parsed to a tree, compiled to a small program and run on a Pike VM over code points:
 * no backtracking and no {@code java.util.regex}, so a match costs O(program x input) whatever the
 * pattern.
 */
public final class Patterns {
  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10000;
  private static final int MAX_COUNT = 100;
  private static final int END = -1;

  private Patterns() {}

  /** Code point ranges as pairs {@code lo, hi}, inclusive, or their complement when negated. */
  private record Item(int[] ranges, boolean negate) {}

  /** A class: any of its items, or none of them when negated. */
  private record CharSet(List<Item> items, boolean negate) {}

  private enum Kind {
    SET,
    START,
    END,
    CONCAT,
    ALT,
    REPEAT
  }

  private static final class Node {
    final Kind kind;
    final int size;
    CharSet set;
    List<Node> parts;
    Node body;
    int min;
    Integer max;

    Node(Kind kind, int size) {
      this.kind = kind;
      this.size = size;
    }
  }

  private static final class Invalid extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Invalid() {
      super(null, null, false, false);
    }
  }

  private static int saturate(long size) {
    return (int) Math.min(size, MAX_SIZE + 1L);
  }

  private static int[] codePoints(String text) {
    int[] points = new int[text.length()];
    int n = 0;
    for (int i = 0; i < text.length(); i++) {
      char high = text.charAt(i);
      if (Character.isHighSurrogate(high)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        points[n++] = Character.toCodePoint(high, text.charAt(i + 1));
        i++;
      } else {
        points[n++] = high;
      }
    }
    return Arrays.copyOf(points, n);
  }

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  private static Node setOf(Item item) {
    Node node = new Node(Kind.SET, 1);
    node.set = new CharSet(List.of(item), false);
    return node;
  }

  private static final Item DIGIT = new Item(new int[] {0x30, 0x39}, false);
  private static final Item WORD =
      new Item(new int[] {0x30, 0x39, 0x41, 0x5a, 0x5f, 0x5f, 0x61, 0x7a}, false);
  private static final Item SPACE = new Item(new int[] {0x09, 0x0d, 0x20, 0x20}, false);
  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";

  private static Item negated(Item item) {
    return new Item(item.ranges(), true);
  }

  /** What an escape letter stands for: a code point, or a class. */
  private static Object escapeFor(int c) {
    switch (c) {
      case 'n':
        return 0x0a;
      case 't':
        return 0x09;
      case 'r':
        return 0x0d;
      case 'f':
        return 0x0c;
      case 'v':
        return 0x0b;
      case 'd':
        return DIGIT;
      case 'w':
        return WORD;
      case 's':
        return SPACE;
      case 'D':
        return negated(DIGIT);
      case 'W':
        return negated(WORD);
      case 'S':
        return negated(SPACE);
      default:
        return null;
    }
  }

  private static boolean isDigit(int c) {
    return c >= '0' && c <= '9';
  }

  private static final class Parser {
    private final int[] cps;
    private int pos;

    Parser(int[] cps) {
      this.cps = cps;
    }

    Node parse() {
      Node node = alternation();
      if (pos < cps.length) {
        throw new Invalid();
      }
      return node;
    }

    private int peek(int offset) {
      int at = pos + offset;
      return at < cps.length ? cps[at] : END;
    }

    private int take() {
      int c = peek(0);
      pos++;
      return c;
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
      long size = sides.size() - 1L;
      for (Node side : sides) {
        size += side.size;
      }
      Node node = new Node(Kind.ALT, saturate(size));
      node.parts = sides;
      return node;
    }

    private Node concat() {
      List<Node> parts = new ArrayList<>();
      long size = 0;
      for (int c = peek(0); c != END && c != '|' && c != ')'; c = peek(0)) {
        Node atom = atom();
        boolean anchor = atom.kind == Kind.START || atom.kind == Kind.END;
        int next = peek(0);
        boolean quantifier = next == '*' || next == '+' || next == '?' || next == '{';
        Node part = !anchor && quantifier ? quantify(atom) : atom;
        parts.add(part);
        size += part.size;
      }
      Node node = new Node(Kind.CONCAT, saturate(size));
      node.parts = parts;
      return node;
    }

    private Node atom() {
      int c = take();
      if (c == '(') {
        Node inner = alternation();
        if (take() != ')') {
          throw new Invalid();
        }
        return inner;
      }
      if (c == '[') {
        Node node = new Node(Kind.SET, 1);
        node.set = charClass();
        return node;
      }
      if (c == '\\') {
        return setOf(asItem(escape()));
      }
      if (c == '*' || c == '+' || c == '?' || c == '{' || c == '}' || c == ']') {
        throw new Invalid();
      }
      if (c == END) {
        throw new Invalid();
      }
      if (c == '.') {
        return setOf(new Item(new int[] {0x0a, 0x0a}, true));
      }
      if (c == '^') {
        return new Node(Kind.START, 1);
      }
      if (c == '$') {
        return new Node(Kind.END, 1);
      }
      return setOf(single(c));
    }

    private Object escape() {
      int c = take();
      if (c != END && ESCAPABLE.indexOf(c) >= 0) {
        return c;
      }
      Object e = escapeFor(c);
      if (e == null) {
        throw new Invalid();
      }
      return e;
    }

    private CharSet charClass() {
      boolean negate = peek(0) == '^';
      if (negate) {
        pos++;
      }
      List<Item> items = new ArrayList<>();
      items.add(member(true));
      while (peek(0) != ']') {
        items.add(member(false));
      }
      pos++;
      return new CharSet(items, negate);
    }

    private Item member(boolean first) {
      Object from = classAtom(first);
      boolean range = peek(0) == '-' && peek(1) != END && peek(1) != ']';
      if (!range) {
        return asItem(from);
      }
      pos++;
      Object to = classAtom(false);
      if (!(from instanceof Integer f) || !(to instanceof Integer t) || f > t) {
        throw new Invalid();
      }
      return new Item(new int[] {f, t}, false);
    }

    private Object classAtom(boolean first) {
      int c = take();
      if (c == END || c == '[' || c == ']') {
        throw new Invalid();
      }
      if (c == '\\') {
        return escape();
      }
      if (c == '-' && !first && peek(0) != ']') {
        throw new Invalid();
      }
      return c;
    }

    private Node quantify(Node body) {
      int c = take();
      if (c != '{') {
        Node node = new Node(Kind.REPEAT, saturate(body.size + 1L));
        node.body = body;
        node.min = c == '+' ? 1 : 0;
        node.max = c == '?' ? Integer.valueOf(1) : null;
        return node;
      }
      int min = count();
      Integer max = upper(min);
      if (take() != '}') {
        throw new Invalid();
      }
      if (max == null) {
        return repeat(body, min, null, min + 1L);
      }
      if (min > max) {
        throw new Invalid();
      }
      return repeat(body, min, max, max);
    }

    private Integer upper(int min) {
      if (peek(0) != ',') {
        return min;
      }
      pos++;
      return peek(0) == '}' ? null : Integer.valueOf(count());
    }

    private Node repeat(Node body, int min, Integer max, long times) {
      Node node = new Node(Kind.REPEAT, Math.max(1, saturate(times * body.size)));
      node.body = body;
      node.min = min;
      node.max = max;
      return node;
    }

    private int count() {
      int c = take();
      if (!isDigit(c)) {
        throw new Invalid();
      }
      if (c == '0') {
        if (isDigit(peek(0))) {
          throw new Invalid();
        }
        return 0;
      }
      int value = c - '0';
      while (isDigit(peek(0))) {
        value = value * 10 + (take() - '0');
        if (value > MAX_COUNT) {
          throw new Invalid();
        }
      }
      return value;
    }
  }

  private static Item asItem(Object atom) {
    return atom instanceof Integer c ? single(c) : (Item) atom;
  }

  private static Node parsePattern(int[] cps) {
    if (cps.length > MAX_LENGTH) {
      return null;
    }
    try {
      Node node = new Parser(cps).parse();
      return node.size > MAX_SIZE ? null : node;
    } catch (Invalid e) {
      return null;
    }
  }

  private enum Op {
    CHAR,
    SPLIT,
    START,
    END,
    MATCH
  }

  private static final class Inst {
    final Op op;
    CharSet set;
    int x;
    int y;

    Inst(Op op) {
      this.op = op;
    }
  }

  /** A compiled pattern. */
  public static final class Compiled {
    private final Inst[] program;

    private Compiled(Inst[] program) {
      this.program = program;
    }
  }

  private static Inst split(List<Inst> program, int x) {
    Inst inst = new Inst(Op.SPLIT);
    inst.x = x;
    inst.y = x;
    program.add(inst);
    return inst;
  }

  private static void emitAlt(List<Node> sides, List<Inst> program) {
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

  private static void emitRepeat(Node body, int min, Integer max, List<Inst> program) {
    if (body.size == 0) {
      return;
    }
    for (int i = 0; i < min; i++) {
      emit(body, program);
    }
    if (max == null) {
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
        {
          Inst inst = new Inst(Op.CHAR);
          inst.set = node.set;
          program.add(inst);
          return;
        }
      case START:
        program.add(new Inst(Op.START));
        return;
      case END:
        program.add(new Inst(Op.END));
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
        throw new IllegalStateException();
    }
  }

  /**
   * Compiles a pattern.
   *
   * @param pattern the pattern
   * @return the program, or null when the pattern is not valid
   */
  public static Compiled compile(String pattern) {
    Node node = parsePattern(codePoints(pattern));
    if (node == null) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(Op.MATCH));
    return new Compiled(program.toArray(new Inst[0]));
  }

  private static boolean inItem(Item item, int c) {
    boolean hit = false;
    int[] r = item.ranges();
    for (int i = 0; i + 1 < r.length && !hit; i += 2) {
      hit = r[i] <= c && c <= r[i + 1];
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

  /** The state of one match: Pike VM threads per position. */
  private static final class Run {
    final Inst[] program;
    final int length;
    final int[] seen;
    final int[] stack;
    int top;

    Run(Inst[] program, int length) {
      this.program = program;
      this.length = length;
      this.seen = new int[program.length];
      Arrays.fill(seen, -1);
      // A pc is pushed at most twice per position (as a target of two splits is not possible
      // without being marked seen first), so program.length * 2 + 2 bounds the stack.
      this.stack = new int[program.length * 2 + 2];
    }

    /** Adds {@code pc} and what it reaches without reading; true when that reaches a match. */
    boolean add(int[] list, int[] count, int pc, int pos) {
      top = 0;
      stack[top++] = pc;
      while (top > 0) {
        int at = stack[--top];
        if (at < 0 || at >= program.length) {
          continue;
        }
        Inst inst = program[at];
        if (seen[at] == pos) {
          continue;
        }
        seen[at] = pos;
        switch (inst.op) {
          case MATCH:
            top = 0;
            return true;
          case CHAR:
            list[count[0]++] = at;
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
            break;
        }
      }
      return false;
    }
  }

  /**
   * Tells whether the pattern matches somewhere in the text.
   *
   * @param compiled the compiled pattern
   * @param text the text
   * @return whether it matches
   */
  public static boolean matches(Compiled compiled, String text) {
    int[] input = codePoints(text);
    Run run = new Run(compiled.program, input.length);
    int n = compiled.program.length;
    int[] current = new int[n];
    int[] next = new int[n];
    int[] currentCount = {0};
    int[] nextCount = {0};
    for (int pos = 0; ; pos++) {
      if (run.add(current, currentCount, 0, pos)) {
        return true;
      }
      if (pos >= input.length) {
        return false;
      }
      int c = input[pos];
      nextCount[0] = 0;
      for (int i = 0; i < currentCount[0]; i++) {
        int pc = current[i];
        Inst inst = compiled.program[pc];
        if (inst.op == Op.CHAR && inSet(inst.set, c) && run.add(next, nextCount, pc + 1, pos + 1)) {
          return true;
        }
      }
      int[] swap = current;
      current = next;
      next = swap;
      int[] swapCount = currentCount;
      currentCount = nextCount;
      nextCount = swapCount;
    }
  }
}
