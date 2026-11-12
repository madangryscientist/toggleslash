package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * The parser for {@code matches} patterns, to "Patterns" in the evaluation spec: code points in, a
 * tree with each node's size out, or null for a pattern that is not valid. {@link PatternProgram}
 * compiles the tree and runs it.
 */
final class PatternParser {

  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10_000;
  private static final int MAX_COUNT = 100;
  private static final int END = -1;
  private static final int NEWLINE = 0x0A;

  /** Code point ranges as pairs lo, hi (inclusive), or their complement when negated. */
  record Item(int[] ranges, boolean negate) {}

  /** A class: any of its items, or none of them when negated. */
  record CharSet(List<Item> items, boolean negate) {}

  /** A parsed node and its size. */
  sealed interface Node permits SetNode, AnchorNode, Concat, Alt, Repeat {
    int size();
  }

  record SetNode(CharSet set) implements Node {
    @Override
    public int size() {
      return 1;
    }
  }

  /** {@code ^} or {@code $}. */
  record AnchorNode(boolean atStart) implements Node {
    @Override
    public int size() {
      return 1;
    }
  }

  record Concat(List<Node> parts, int size) implements Node {}

  record Alt(List<Node> sides, int size) implements Node {}

  /** {@code max} is -1 for no upper bound. */
  record Repeat(Node body, int min, int max, int size) implements Node {}

  /** One atom inside a class or after a backslash: a single code point, or a shorthand class. */
  private record Atom(int codePoint, Item item) {
    static Atom of(int codePoint) {
      return new Atom(codePoint, null);
    }

    static Atom of(Item item) {
      return new Atom(0, item);
    }

    Item asItem() {
      return item != null ? item : new Item(new int[] {codePoint, codePoint}, false);
    }
  }

  private static final class Invalid extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Invalid() {
      super(null, null, false, false);
    }
  }

  private static final Item DIGIT = new Item(new int[] {0x30, 0x39}, false);
  private static final Item WORD =
      new Item(new int[] {0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A}, false);

  /** U+0009 to U+000D are \t \n \v \f \r. */
  private static final Item SPACE = new Item(new int[] {0x09, 0x0D, 0x20, 0x20}, false);

  private final int[] cps;
  private int pos;

  private PatternParser(int[] cps) {
    this.cps = cps;
  }

  /** The tree for a pattern's code points, or null when it is too long, malformed or too big. */
  static Node parse(String pattern) {
    int[] cps = pattern.codePoints().toArray();
    if (cps.length > MAX_LENGTH) {
      return null;
    }
    try {
      PatternParser parser = new PatternParser(cps);
      Node node = parser.alternation();
      if (parser.pos < cps.length) {
        return null; // an unmatched ')'
      }
      return node.size() > MAX_SIZE ? null : node;
    } catch (Invalid e) {
      return null;
    }
  }

  private static int saturate(long size) {
    return (int) Math.min(size, MAX_SIZE + 1L);
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
      size += side.size();
    }
    return new Alt(sides, saturate(size));
  }

  private Node concat() {
    List<Node> parts = new ArrayList<>();
    long size = 0;
    for (int c = peek(0); c != END && c != '|' && c != ')'; c = peek(0)) {
      Node atom = atom();
      int next = peek(0);
      boolean quantifier = next == '*' || next == '+' || next == '?' || next == '{';
      Node part = !(atom instanceof AnchorNode) && quantifier ? quantify(atom) : atom;
      parts.add(part);
      size += part.size();
    }
    return new Concat(parts, saturate(size));
  }

  private Node atom() {
    int c = take();
    switch (c) {
      case '(':
        return group();
      case '[':
        return new SetNode(charClass());
      case '\\':
        return new SetNode(single(escape().asItem()));
      case '*':
      case '+':
      case '?':
      case '{':
      case '}':
      case ']':
        throw new Invalid();
      case '.':
        return new SetNode(single(new Item(new int[] {NEWLINE, NEWLINE}, true)));
      case '^':
        return new AnchorNode(true);
      case '$':
        return new AnchorNode(false);
      default:
        return new SetNode(single(Atom.of(c).asItem()));
    }
  }

  private static CharSet single(Item item) {
    return new CharSet(List.of(item), false);
  }

  private Node group() {
    Node inner = alternation();
    if (take() != ')') {
      throw new Invalid();
    }
    return inner;
  }

  /** After a backslash: one code point, or one of the six shorthand classes. */
  private Atom escape() {
    int c = take();
    switch (c) {
      case '\\':
      case '.':
      case '[':
      case ']':
      case '(':
      case ')':
      case '|':
      case '*':
      case '+':
      case '?':
      case '{':
      case '}':
      case '^':
      case '$':
      case '-':
        return Atom.of(c);
      case 'n':
        return Atom.of(0x0A);
      case 't':
        return Atom.of(0x09);
      case 'r':
        return Atom.of(0x0D);
      case 'f':
        return Atom.of(0x0C);
      case 'v':
        return Atom.of(0x0B);
      case 'd':
        return Atom.of(DIGIT);
      case 'w':
        return Atom.of(WORD);
      case 's':
        return Atom.of(SPACE);
      case 'D':
        return Atom.of(new Item(DIGIT.ranges(), true));
      case 'W':
        return Atom.of(new Item(WORD.ranges(), true));
      case 'S':
        return Atom.of(new Item(SPACE.ranges(), true));
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
    if (from.item() != null || to.item() != null || from.codePoint() > to.codePoint()) {
      throw new Invalid();
    }
    return new Item(new int[] {from.codePoint(), to.codePoint()}, false);
  }

  private Atom classAtom(boolean first) {
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
    return Atom.of(c);
  }

  private Node quantify(Node body) {
    int c = take();
    if (c != '{') {
      int min = c == '+' ? 1 : 0;
      int max = c == '?' ? 1 : -1;
      return new Repeat(body, min, max, saturate(body.size() + 1L));
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

  /** The upper bound of a counted repeat: n again for a bare count, -1 when open ended, else m. */
  private int upper(int min) {
    if (peek(0) != ',') {
      return min;
    }
    pos++;
    return peek(0) == '}' ? -1 : count();
  }

  private static Node repeat(Node body, int min, int max, long times) {
    return new Repeat(body, min, max, Math.max(1, saturate(times * body.size())));
  }

  private static boolean isDigit(int c) {
    return c >= '0' && c <= '9';
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
