package com.github.madangryscientist.toggleslash.sdks.java.eval;

import java.util.ArrayList;
import java.util.List;

/**
 * The parser for {@code matches} patterns, to the "Patterns" section of {@code docs/evaluation.md}:
 * code points in, a tree with each node's size out, or null for a pattern that is not valid.
 */
final class PatternParser {
  static final int MAX_LENGTH = 1024;
  static final int MAX_SIZE = 10000;
  static final int MAX_COUNT = 100;
  private static final int END = -1;

  /** Code point ranges as pairs, inclusive, or their complement when negated. */
  record Item(int[] ranges, boolean negate) {}

  /** A class: any of its items, or none of them when negated. */
  record CharSet(List<Item> items, boolean negate) {}

  /** A node of the tree. */
  static final class Node {
    static final int SET = 0;
    static final int START = 1;
    static final int END_ANCHOR = 2;
    static final int CONCAT = 3;
    static final int ALT = 4;
    static final int REPEAT = 5;

    final int kind;
    final int size;
    CharSet set;
    List<Node> parts;
    Node body;
    int min;

    /** The upper bound, or -1 for none. */
    int max;

    Node(int kind, int size) {
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

  private static final Item DIGIT = new Item(new int[] {0x30, 0x39}, false);
  private static final Item WORD =
      new Item(new int[] {0x30, 0x39, 0x41, 0x5a, 0x5f, 0x5f, 0x61, 0x7a}, false);
  private static final Item SPACE = new Item(new int[] {0x09, 0x0d, 0x20, 0x20}, false);
  private static final int NEWLINE = 0x0a;
  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";
  private static final String NOT_AN_ATOM = "*+?{}]";
  private static final String QUANTIFIERS = "*+?{";

  private final int[] cps;
  private int pos;

  private PatternParser(int[] cps) {
    this.cps = cps;
  }

  /**
   * The tree for a pattern, or null when it is not valid: too long, malformed or too big.
   *
   * @param pattern the pattern
   * @return the tree, or null
   */
  static Node parse(String pattern) {
    int[] points = pattern.codePoints().toArray();
    if (points.length > MAX_LENGTH) {
      return null;
    }
    try {
      PatternParser parser = new PatternParser(points);
      Node node = parser.alternation();
      if (parser.pos < points.length) {
        return null;
      }
      return node.size > MAX_SIZE ? null : node;
    } catch (Invalid e) {
      return null;
    }
  }

  private static int saturate(long size) {
    return (int) Math.min(size, MAX_SIZE + 1L);
  }

  private static Invalid fail() {
    return new Invalid();
  }

  private static boolean isDigit(int c) {
    return c >= 0x30 && c <= 0x39;
  }

  private static boolean in(String set, int c) {
    return c >= 0 && c < 0x80 && set.indexOf(c) >= 0;
  }

  private int peek() {
    return peek(0);
  }

  private int peek(int offset) {
    int at = pos + offset;
    return at < cps.length ? cps[at] : END;
  }

  private int take() {
    int c = peek();
    pos++;
    return c;
  }

  private static Node setNode(Item item) {
    Node node = new Node(Node.SET, 1);
    node.set = new CharSet(List.of(item), false);
    return node;
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
    long size = sides.size() - 1L;
    for (Node side : sides) {
      size += side.size;
    }
    Node node = new Node(Node.ALT, saturate(size));
    node.parts = sides;
    return node;
  }

  private Node concat() {
    List<Node> parts = new ArrayList<>();
    for (int c = peek(); c != END && c != '|' && c != ')'; c = peek()) {
      Node atom = atom();
      boolean anchor = atom.kind == Node.START || atom.kind == Node.END_ANCHOR;
      parts.add(!anchor && in(QUANTIFIERS, peek()) ? quantify(atom) : atom);
    }
    long size = 0;
    for (Node part : parts) {
      size += part.size;
    }
    Node node = new Node(Node.CONCAT, saturate(size));
    node.parts = parts;
    return node;
  }

  private Node atom() {
    int c = take();
    if (c == '(') {
      return group();
    }
    if (c == '[') {
      Node node = new Node(Node.SET, 1);
      node.set = charClass();
      return node;
    }
    if (c == '\\') {
      return setNode(asItem(escape()));
    }
    if (in(NOT_AN_ATOM, c)) {
      throw fail();
    }
    if (c == '.') {
      return setNode(new Item(new int[] {NEWLINE, NEWLINE}, true));
    }
    if (c == '^') {
      return new Node(Node.START, 1);
    }
    if (c == '$') {
      return new Node(Node.END_ANCHOR, 1);
    }
    return setNode(single(c));
  }

  private Node group() {
    Node inner = alternation();
    if (take() != ')') {
      throw fail();
    }
    return inner;
  }

  /** After a backslash: one code point as a boxed int, or one of the six shorthand classes. */
  private Object escape() {
    int c = take();
    if (in(ESCAPABLE, c)) {
      return c;
    }
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
        return new Item(DIGIT.ranges(), true);
      case 'W':
        return new Item(WORD.ranges(), true);
      case 'S':
        return new Item(SPACE.ranges(), true);
      default:
        throw fail();
    }
  }

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  private static Item asItem(Object atom) {
    return atom instanceof Integer c ? single(c) : (Item) atom;
  }

  /** After an opening bracket: the members up to the closing one. */
  private CharSet charClass() {
    boolean negate = peek() == '^';
    if (negate) {
      pos++;
    }
    List<Item> items = new ArrayList<>();
    items.add(member(true));
    while (peek() != ']') {
      items.add(member(false));
    }
    pos++;
    return new CharSet(items, negate);
  }

  private Item member(boolean first) {
    Object from = classAtom(first);
    boolean range = peek() == '-' && peek(1) != END && peek(1) != ']';
    if (!range) {
      return asItem(from);
    }
    pos++;
    Object to = classAtom(false);
    if (!(from instanceof Integer lo) || !(to instanceof Integer hi) || lo > hi) {
      throw fail();
    }
    return new Item(new int[] {lo, hi}, false);
  }

  private Object classAtom(boolean first) {
    int c = take();
    if (c == END || c == '[' || c == ']') {
      throw fail();
    }
    if (c == '\\') {
      return escape();
    }
    if (c == '-' && !first && peek() != ']') {
      throw fail();
    }
    return c;
  }

  private Node quantify(Node body) {
    int c = take();
    if (c != '{') {
      int min = c == '+' ? 1 : 0;
      int max = c == '?' ? 1 : -1;
      return repeat(body, min, max, saturate(body.size + 1L));
    }
    int min = count();
    int max = upper(min);
    if (take() != '}') {
      throw fail();
    }
    if (max < 0) {
      return repeat(body, min, -1, Math.max(1, saturate((min + 1L) * body.size)));
    }
    if (min > max) {
      throw fail();
    }
    return repeat(body, min, max, Math.max(1, saturate((long) max * body.size)));
  }

  private static Node repeat(Node body, int min, int max, int size) {
    Node node = new Node(Node.REPEAT, size);
    node.body = body;
    node.min = min;
    node.max = max;
    return node;
  }

  /**
   * After the first count: the same count for a single bound, -1 for an open one, or the second.
   */
  private int upper(int min) {
    if (peek() != ',') {
      return min;
    }
    pos++;
    return peek() == '}' ? -1 : count();
  }

  /** ASCII digits with no leading zero, at most 100. */
  private int count() {
    int c = take();
    if (!isDigit(c)) {
      throw fail();
    }
    if (c == '0') {
      if (isDigit(peek())) {
        throw fail();
      }
      return 0;
    }
    int value = c - 0x30;
    while (isDigit(peek())) {
      value = value * 10 + (take() - 0x30);
      if (value > MAX_COUNT) {
        throw fail();
      }
    }
    return value;
  }
}
