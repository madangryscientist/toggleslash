package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * Patterns for the {@code matches} op, to the "Patterns" section of the evaluation spec: a parser
 * to a tree, a compiler to a small program, and a Pike VM over code points. There is no
 * backtracking and no {@code java.util.regex}, so a match costs O(program x input) whatever the
 * pattern.
 */
final class Pattern {
  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10000;
  private static final int MAX_COUNT = 100;
  private static final int END = -1;

  // ---- the tree ----

  /** Code point ranges as pairs lo, hi (inclusive), or their complement when negated. */
  private static final class Item {
    final int[] ranges;
    final boolean negate;

    Item(int[] ranges, boolean negate) {
      this.ranges = ranges;
      this.negate = negate;
    }

    boolean contains(int c) {
      boolean hit = false;
      for (int i = 0; i + 1 < ranges.length && !hit; i += 2) {
        hit = ranges[i] <= c && c <= ranges[i + 1];
      }
      return hit != negate;
    }
  }

  /** A class: any of its items, or none of them when negated. */
  private static final class CharSet {
    final List<Item> items;
    final boolean negate;

    CharSet(List<Item> items, boolean negate) {
      this.items = items;
      this.negate = negate;
    }

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

    /** -1 for no upper bound. */
    int max;

    Node(Kind kind, int size) {
      this.kind = kind;
      this.size = size;
    }
  }

  /**
   * The code points of a UTF-16 string: a valid surrogate pair is one, a lone surrogate is itself.
   */
  static int[] codePoints(String text) {
    int[] points = new int[text.length()];
    int n = 0;
    for (int i = 0; i < text.length(); i++) {
      char high = text.charAt(i);
      char low = i + 1 < text.length() ? text.charAt(i + 1) : 0;
      boolean pair = Character.isHighSurrogate(high) && Character.isLowSurrogate(low);
      points[n++] = pair ? Character.toCodePoint(high, low) : high;
      if (pair) {
        i++;
      }
    }
    return java.util.Arrays.copyOf(points, n);
  }

  private static final Item DIGIT = new Item(new int[] {'0', '9'}, false);
  private static final Item WORD =
      new Item(new int[] {'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}, false);
  // U+0009 to U+000D are \t \n \v \f \r.
  private static final Item SPACE = new Item(new int[] {0x09, 0x0d, 0x20, 0x20}, false);

  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";

  private static final class Invalid extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Invalid() {
      super(null, null, false, false);
    }
  }

  private static Node setOf(Item item) {
    Node node = new Node(Kind.SET, 1);
    node.set = new CharSet(List.of(item), false);
    return node;
  }

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  private static int saturate(long size) {
    return (int) Math.min(size, MAX_SIZE + 1L);
  }

  private static boolean isDigit(int c) {
    return c >= '0' && c <= '9';
  }

  /** An escape result: one code point, or one of the six shorthand classes. */
  private static final class Atom {
    final int codePoint;
    final Item item;

    Atom(int codePoint, Item item) {
      this.codePoint = codePoint;
      this.item = item;
    }

    Item asItem() {
      return item != null ? item : single(codePoint);
    }
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
        throw new Invalid(); // an unmatched close parenthesis
      }
      return node;
    }

    private int peek(int offset) {
      return pos + offset < cps.length ? cps[pos + offset] : END;
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
      for (int c = peek(0); c != END && c != '|' && c != ')'; c = peek(0)) {
        Node atom = atom();
        boolean anchor = atom.kind == Kind.START || atom.kind == Kind.END;
        int next = peek(0);
        boolean quantifier = next == '*' || next == '+' || next == '?' || next == '{';
        parts.add(!anchor && quantifier ? quantify(atom) : atom);
      }
      long size = 0;
      for (Node part : parts) {
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
        return setOf(escape().asItem());
      }
      if (c == '*' || c == '+' || c == '?' || c == '{' || c == '}' || c == ']' || c == END) {
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

    /** After a backslash: one code point, or one of the six shorthand classes. */
    private Atom escape() {
      int c = take();
      if (c != END && ESCAPABLE.indexOf(c) >= 0) {
        return new Atom(c, null);
      }
      switch (c) {
        case 'n':
          return new Atom(0x0a, null);
        case 't':
          return new Atom(0x09, null);
        case 'r':
          return new Atom(0x0d, null);
        case 'f':
          return new Atom(0x0c, null);
        case 'v':
          return new Atom(0x0b, null);
        case 'd':
          return new Atom(0, DIGIT);
        case 'w':
          return new Atom(0, WORD);
        case 's':
          return new Atom(0, SPACE);
        case 'D':
          return new Atom(0, new Item(DIGIT.ranges, true));
        case 'W':
          return new Atom(0, new Item(WORD.ranges, true));
        case 'S':
          return new Atom(0, new Item(SPACE.ranges, true));
        default:
          throw new Invalid();
      }
    }

    /** After an opening bracket: the members up to the closing one. */
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

    /** One member: an atom, or a range of two single code points. */
    private Item member(boolean first) {
      Atom from = classAtom(first);
      boolean range = peek(0) == '-' && peek(1) != END && peek(1) != ']';
      if (!range) {
        return from.asItem();
      }
      pos++;
      Atom to = classAtom(false);
      if (from.item != null || to.item != null || from.codePoint > to.codePoint) {
        throw new Invalid();
      }
      return new Item(new int[] {from.codePoint, to.codePoint}, false);
    }

    private Atom classAtom(boolean first) {
      int c = take();
      // A closing bracket here is a class with no members; the end and an opening bracket are never
      // members.
      if (c == END || c == '[' || c == ']') {
        throw new Invalid();
      }
      if (c == '\\') {
        return escape();
      }
      // An unescaped hyphen is a literal only as the first member or directly before a closing
      // bracket.
      if (c == '-' && !first && peek(0) != ']') {
        throw new Invalid();
      }
      return new Atom(c, null);
    }

    private Node quantify(Node body) {
      int c = take();
      if (c != '{') {
        Node node = new Node(Kind.REPEAT, saturate(body.size + 1L));
        node.body = body;
        node.min = c == '+' ? 1 : 0;
        node.max = c == '?' ? 1 : -1;
        return node;
      }
      int min = count();
      int max = upper(min);
      if (take() != '}') {
        throw new Invalid();
      }
      if (max == -1) {
        return repeat(body, min, -1, min + 1L);
      }
      if (min > max) {
        throw new Invalid();
      }
      return repeat(body, min, max, max);
    }

    /**
     * After the opening brace of a bound: n again for the form with just n, -1 for the form with n
     * and a comma, m for the form with n and m. Leaves the closing brace.
     */
    private int upper(int min) {
      if (peek(0) != ',') {
        return min;
      }
      pos++;
      return peek(0) == '}' ? -1 : count();
    }

    private Node repeat(Node body, int min, int max, long times) {
      Node node = new Node(Kind.REPEAT, Math.max(1, saturate(times * body.size)));
      node.body = body;
      node.min = min;
      node.max = max;
      return node;
    }

    /** ASCII digits with no leading zero, at most 100. */
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

  private static Node parseTree(int[] cps) {
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

  // ---- the program ----

  private static final int CHAR = 0;
  private static final int SPLIT = 1;
  private static final int START = 2;
  private static final int ENDOP = 3;
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

  private Pattern(Inst[] program) {
    this.program = program;
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
        program.add(new Inst(ENDOP, null, 0));
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
      default:
        emitRepeat(node.body, node.min, node.max, program);
    }
  }

  /** The compiled pattern, or null when {@code pattern} is not a valid pattern. */
  static Pattern compile(String pattern) {
    Node node = parseTree(codePoints(pattern));
    if (node == null) {
      return null;
    }
    List<Inst> program = new ArrayList<>();
    emit(node, program);
    program.add(new Inst(MATCH, null, 0));
    return new Pattern(program.toArray(new Inst[0]));
  }

  // ---- the machine ----

  /**
   * Does the pattern match somewhere in {@code text}? Each instruction is visited once per
   * position.
   */
  boolean matches(String text) {
    int[] input = codePoints(text);
    int length = input.length;
    int[] seen = new int[program.length];
    java.util.Arrays.fill(seen, -1);
    int[] stack = new int[program.length * 2 + 2];
    int[] current = new int[program.length];
    int[] next = new int[program.length];
    int currentSize = 0;
    for (int pos = 0; ; pos++) {
      int nextSize = 0;
      // Add the start of the program at this position, then step the threads over the character.
      int[] listForStart = current;
      int startSize = currentSize;
      int added = add(listForStart, startSize, 0, pos, length, seen, stack);
      if (added < 0) {
        return true;
      }
      currentSize = added;
      if (pos >= length) {
        return false;
      }
      int c = input[pos];
      for (int i = 0; i < currentSize; i++) {
        Inst inst = program[current[i]];
        if (inst.set.contains(c)) {
          int grown = add(next, nextSize, current[i] + 1, pos + 1, length, seen, stack);
          if (grown < 0) {
            return true;
          }
          nextSize = grown;
        }
      }
      int[] swap = current;
      current = next;
      next = swap;
      currentSize = nextSize;
    }
  }

  /**
   * Adds {@code pc} and everything it reaches without reading to {@code list} (of {@code size}
   * entries) at {@code pos}. Returns the new size, or -1 when that reaches a match.
   */
  private int add(int[] list, int size, int pc, int pos, int length, int[] seen, int[] stack) {
    int sp = 0;
    int n = size;
    stack[sp++] = pc;
    while (sp > 0) {
      int at = stack[--sp];
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
      }
    }
    return n;
  }
}
