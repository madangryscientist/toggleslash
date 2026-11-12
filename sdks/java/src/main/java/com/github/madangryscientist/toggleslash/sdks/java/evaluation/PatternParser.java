package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * The parser for {@code matches} patterns, to the "Patterns" section of the spec: code points in, a
 * tree with each node's size out, or null for a pattern that is not valid.
 */
final class PatternParser {
  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10000;
  private static final int MAX_COUNT = 100;
  private static final int END = -1;
  private static final int NEWLINE = 0x0A;

  /** Code point ranges as pairs {@code lo, hi}, inclusive, or their complement when negated. */
  record Item(int[] ranges, boolean negate) {}

  /** A class: any of its items, or none of them when negated. */
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

  /** A node of the tree; the fields that do not apply to the kind are null or zero. */
  static final class Node {
    final Kind kind;
    final int size;
    CharSet set;
    List<Node> parts;
    Node body;
    int min;
    int max;

    Node(Kind kind, int size) {
      this.kind = kind;
      this.size = size;
    }
  }

  private static final Item DIGIT = new Item(new int[] {'0', '9'}, false);
  private static final Item WORD =
      new Item(new int[] {'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}, false);
  // U+0009 to U+000D are \t \n \v \f \r.
  private static final Item SPACE = new Item(new int[] {0x09, 0x0D, 0x20, 0x20}, false);
  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";
  private static final String NOT_AN_ATOM = "*+?{}]";
  private static final String QUANTIFIERS = "*+?{";

  /** Thrown inside the parser for a pattern that is not valid. */
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
        return null;
      }
      return node.size > MAX_SIZE ? null : node;
    } catch (Invalid e) {
      return null;
    }
  }

  private static Invalid fail() {
    return new Invalid();
  }

  private static int saturate(int size) {
    return Math.min(size, MAX_SIZE + 1);
  }

  private static boolean isIn(String chars, int c) {
    return c >= 0 && c < 0x10000 && chars.indexOf((char) c) >= 0;
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

  private static Node setOf(Item item) {
    Node node = new Node(Kind.SET, 1);
    node.set = new CharSet(List.of(item), false);
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
    int size = sides.size() - 1;
    for (Node side : sides) {
      size += side.size;
    }
    Node node = new Node(Kind.ALT, saturate(size));
    node.parts = sides;
    return node;
  }

  private Node concat() {
    List<Node> parts = new ArrayList<>();
    int size = 0;
    for (int c = peek(0); c != END && c != '|' && c != ')'; c = peek(0)) {
      Node atom = atom();
      boolean anchor = atom.kind == Kind.START || atom.kind == Kind.END;
      Node part = !anchor && isIn(QUANTIFIERS, peek(0)) ? quantify(atom) : atom;
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
        throw fail();
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
    if (isIn(NOT_AN_ATOM, c)) {
      throw fail();
    }
    if (c == '.') {
      return setOf(new Item(new int[] {NEWLINE, NEWLINE}, true));
    }
    if (c == '^') {
      return new Node(Kind.START, 1);
    }
    if (c == '$') {
      return new Node(Kind.END, 1);
    }
    return setOf(single(c));
  }

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  private static Item asItem(Object atom) {
    return atom instanceof Integer i ? single(i) : (Item) atom;
  }

  private static Item negated(Item item) {
    return new Item(item.ranges, true);
  }

  /**
   * After a backslash: one code point (an Integer), or one of the six shorthand classes (an Item).
   */
  private Object escape() {
    int c = take();
    if (isIn(ESCAPABLE, c)) {
      return c;
    }
    switch (c) {
      case 'n':
        return 0x0A;
      case 't':
        return 0x09;
      case 'r':
        return 0x0D;
      case 'f':
        return 0x0C;
      case 'v':
        return 0x0B;
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
        throw fail();
    }
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
      throw fail();
    }
    return new Item(new int[] {f, t}, false);
  }

  private Object classAtom(boolean first) {
    int c = take();
    if (c == END || c == '[' || c == ']') {
      throw fail();
    }
    if (c == '\\') {
      return escape();
    }
    if (c == '-' && !first && peek(0) != ']') {
      throw fail();
    }
    return c;
  }

  private Node quantify(Node body) {
    int c = take();
    if (c != '{') {
      int min = c == '+' ? 1 : 0;
      int max = c == '?' ? 1 : -1;
      return repeat(body, min, max, body.size + 1, false);
    }
    int min = count();
    int max = upper(min);
    if (take() != '}') {
      throw fail();
    }
    if (max == -1) {
      return repeat(body, min, -1, min + 1, true);
    }
    if (min > max) {
      throw fail();
    }
    return repeat(body, min, max, max, true);
  }

  /** After the first count: the same for {@code {n}}, -1 for {@code {n,}}, m for {@code {n,m}}. */
  private int upper(int min) {
    if (peek(0) != ',') {
      return min;
    }
    pos++;
    return peek(0) == '}' ? -1 : count();
  }

  private static Node repeat(Node body, int min, int max, int times, boolean scaled) {
    int size = scaled ? Math.max(1, saturate(times * body.size)) : saturate(times);
    Node node = new Node(Kind.REPEAT, size);
    node.body = body;
    node.min = min;
    node.max = max;
    return node;
  }

  /** ASCII digits with no leading zero, at most 100. */
  private int count() {
    int c = take();
    if (c < '0' || c > '9') {
      throw fail();
    }
    if (c == '0') {
      if (peek(0) >= '0' && peek(0) <= '9') {
        throw fail();
      }
      return 0;
    }
    int value = c - '0';
    while (peek(0) >= '0' && peek(0) <= '9') {
      value = value * 10 + (take() - '0');
      if (value > MAX_COUNT) {
        throw fail();
      }
    }
    return value;
  }
}
