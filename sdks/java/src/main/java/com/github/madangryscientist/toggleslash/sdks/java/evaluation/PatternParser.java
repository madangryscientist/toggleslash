package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * The parser for {@code matches} patterns, to the "Patterns" section of docs/evaluation.md: code
 * points in, a tree with each node's size out, or null for a pattern that is not valid. {@link
 * Regex} compiles the tree and runs it. No platform regular expression is involved.
 */
final class PatternParser {
  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10000;
  private static final int MAX_COUNT = 100;
  private static final int END = -1;
  private static final int NEWLINE = 0x0a;

  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";
  private static final String NOT_AN_ATOM = "*+?{}]";
  private static final String QUANTIFIERS = "*+?{";

  private static final Item DIGIT = new Item(new int[] {0x30, 0x39}, false);
  private static final Item WORD =
      new Item(new int[] {0x30, 0x39, 0x41, 0x5a, 0x5f, 0x5f, 0x61, 0x7a}, false);
  // U+0009 to U+000D are tab, newline, vertical tab, form feed and carriage return.
  private static final Item SPACE = new Item(new int[] {0x09, 0x0d, 0x20, 0x20}, false);

  /** Code point ranges as pairs lo, hi (inclusive), or their complement when {@code negate}. */
  record Item(int[] ranges, boolean negate) {}

  /** A class: any of its items, or none of them when {@code negate} is set. */
  record CharSet(List<Item> items, boolean negate) {}

  /** The kinds of node. */
  enum Kind {
    SET,
    START,
    END,
    CONCAT,
    ALT,
    REPEAT
  }

  /** A node of the tree. {@code max} is -1 for no upper bound. */
  record Node(Kind kind, CharSet set, List<Node> parts, Node body, int min, int max, int size) {}

  private static final class Invalid extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Invalid() {
      super(null, null, false, false);
    }
  }

  private final int[] cps;
  private int pos;

  private PatternParser(int[] cps) {
    this.cps = cps;
  }

  /** The tree for a pattern's code points, or null when it is too long, malformed or too big. */
  static Node parse(int[] cps) {
    if (cps.length > MAX_LENGTH) {
      return null;
    }
    try {
      PatternParser parser = new PatternParser(cps);
      Node node = parser.alternation();
      if (parser.pos < cps.length) {
        return null; // an unmatched close parenthesis
      }
      return node.size() > MAX_SIZE ? null : node;
    } catch (Invalid e) {
      return null;
    }
  }

  private static int saturate(int size) {
    return Math.min(size, MAX_SIZE + 1);
  }

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  private static Node setOf(Item item) {
    return new Node(Kind.SET, new CharSet(List.of(item), false), null, null, 0, 0, 1);
  }

  private static boolean isDigit(int c) {
    return c >= 0x30 && c <= 0x39;
  }

  private static boolean in(String set, int c) {
    return c >= 0 && c < 0x80 && set.indexOf(c) >= 0;
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

  private static void fail() {
    throw new Invalid();
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
    int size = sides.size() - 1;
    for (Node side : sides) {
      size = saturate(size + side.size());
    }
    return new Node(Kind.ALT, null, sides, null, 0, 0, saturate(size));
  }

  private Node concat() {
    List<Node> parts = new ArrayList<>();
    for (int c = peek(0); c != END && c != '|' && c != ')'; c = peek(0)) {
      Node atom = atom();
      boolean anchor = atom.kind() == Kind.START || atom.kind() == Kind.END;
      parts.add(!anchor && in(QUANTIFIERS, peek(0)) ? quantify(atom) : atom);
    }
    int size = 0;
    for (Node part : parts) {
      size = saturate(size + part.size());
    }
    return new Node(Kind.CONCAT, null, parts, null, 0, 0, size);
  }

  private Node atom() {
    int c = take();
    if (c == '(') {
      Node inner = alternation();
      if (take() != ')') {
        fail();
      }
      return inner;
    }
    if (c == '[') {
      return new Node(Kind.SET, charClass(), null, null, 0, 0, 1);
    }
    if (c == '\\') {
      return setOf(asItem(escape()));
    }
    if (in(NOT_AN_ATOM, c)) {
      fail();
    }
    if (c == '.') {
      return setOf(new Item(new int[] {NEWLINE, NEWLINE}, true));
    }
    if (c == '^') {
      return new Node(Kind.START, null, null, null, 0, 0, 1);
    }
    if (c == '$') {
      return new Node(Kind.END, null, null, null, 0, 0, 1);
    }
    return setOf(single(c));
  }

  /** After a backslash: one code point (an Integer), or one of the six shorthand classes. */
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
        fail();
        return null;
    }
  }

  /** After the open bracket: the members up to the close bracket. */
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
    Object from = classAtom(first);
    boolean range = peek(0) == '-' && peek(1) != END && peek(1) != ']';
    if (!range) {
      return asItem(from);
    }
    pos++;
    Object to = classAtom(false);
    if (!(from instanceof Integer) || !(to instanceof Integer) || (Integer) from > (Integer) to) {
      fail();
    }
    return new Item(new int[] {(Integer) from, (Integer) to}, false);
  }

  private Object classAtom(boolean first) {
    int c = take();
    // A close bracket here is a class with no members; END and an open bracket are never members.
    if (c == END || c == '[' || c == ']') {
      fail();
    }
    if (c == '\\') {
      return escape();
    }
    // An unescaped hyphen is a literal only as the first member or directly before a close bracket.
    if (c == '-' && !first && peek(0) != ']') {
      fail();
    }
    return c;
  }

  private static Item asItem(Object atom) {
    return atom instanceof Integer ? single((Integer) atom) : (Item) atom;
  }

  private Node quantify(Node body) {
    int c = take();
    if (c != '{') {
      int min = c == '+' ? 1 : 0;
      int max = c == '?' ? 1 : -1;
      return new Node(Kind.REPEAT, null, null, body, min, max, saturate(body.size() + 1));
    }
    int min = count();
    int max = upper(min);
    if (take() != '}') {
      fail();
    }
    if (max == -1) {
      return repeat(body, min, -1, min + 1);
    }
    if (min > max) {
      fail();
    }
    return repeat(body, min, max, max);
  }

  /** After the count n: n again for a bare count, -1 for a trailing comma, m for a range. */
  private int upper(int min) {
    if (peek(0) != ',') {
      return min;
    }
    pos++;
    return peek(0) == '}' ? -1 : count();
  }

  private static Node repeat(Node body, int min, int max, int times) {
    int size = Math.max(1, saturate((int) Math.min((long) times * body.size(), MAX_SIZE + 1L)));
    return new Node(Kind.REPEAT, null, null, body, min, max, size);
  }

  /** ASCII digits with no leading zero, at most 100. */
  private int count() {
    int c = take();
    if (!isDigit(c)) {
      fail();
    }
    if (c == '0') {
      if (isDigit(peek(0))) {
        fail();
      }
      return 0;
    }
    int value = c - 0x30;
    while (isDigit(peek(0))) {
      value = value * 10 + (take() - 0x30);
      if (value > MAX_COUNT) {
        fail();
      }
    }
    return value;
  }
}
