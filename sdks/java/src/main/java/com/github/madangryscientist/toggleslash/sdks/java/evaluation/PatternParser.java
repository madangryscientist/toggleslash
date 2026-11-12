package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * The parser for {@code matches} patterns, to the "Patterns" section of the evaluation spec: code
 * points in, a tree with each node's size out, or {@code null} for a pattern that is not valid.
 */
final class PatternParser {
  static final int MAX_LENGTH = 1024;
  static final int MAX_SIZE = 10000;
  static final int MAX_COUNT = 100;
  private static final int END = -1;

  /** Code point ranges as pairs (low, high), inclusive, or their complement when negated. */
  static final class Item {
    final int[] ranges;
    final boolean negate;

    Item(int[] ranges, boolean negate) {
      this.ranges = ranges;
      this.negate = negate;
    }
  }

  /** A class: any of its items, or none of them when negated. */
  static final class CharSet {
    final List<Item> items;
    final boolean negate;

    CharSet(List<Item> items, boolean negate) {
      this.items = items;
      this.negate = negate;
    }
  }

  /** The kinds of node. */
  enum Kind {
    SET,
    START,
    END,
    CONCAT,
    ALT,
    REPEAT
  }

  /** A node of the pattern tree. */
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

  private static final class Invalid extends RuntimeException {
    private static final long serialVersionUID = 1L;

    Invalid() {
      super("invalid pattern", null, false, false);
    }
  }

  private PatternParser() {}

  /** The code points of a UTF-16 string: a valid surrogate pair is one, a lone surrogate itself. */
  static int[] codePoints(String text) {
    int[] points = new int[text.length()];
    int n = 0;
    for (int i = 0; i < text.length(); i++) {
      char high = text.charAt(i);
      char low = i + 1 < text.length() ? text.charAt(i + 1) : 0;
      boolean pair = high >= 0xD800 && high <= 0xDBFF && low >= 0xDC00 && low <= 0xDFFF;
      if (pair) {
        points[n++] = ((high - 0xD800) << 10) + (low - 0xDC00) + 0x10000;
        i++;
      } else {
        points[n++] = high;
      }
    }
    return java.util.Arrays.copyOf(points, n);
  }

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  private static Node setOf(Item item) {
    Node node = new Node(Kind.SET, 1);
    node.set = new CharSet(List.of(item), false);
    return node;
  }

  private static final Item DIGIT = new Item(new int[] {'0', '9'}, false);
  private static final Item WORD =
      new Item(new int[] {'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}, false);
  private static final Item SPACE = new Item(new int[] {0x09, 0x0D, 0x20, 0x20}, false);
  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";
  private static final String NOT_AN_ATOM = "*+?{}]";
  private static final String QUANTIFIERS = "*+?{";

  private static boolean in(String chars, int c) {
    return c >= 0 && c < 0x10000 && chars.indexOf((char) c) >= 0;
  }

  private static int saturate(int size) {
    return Math.min(size, MAX_SIZE + 1);
  }

  private static boolean isDigit(int c) {
    return c >= '0' && c <= '9';
  }

  private static Item asItem(Object atom) {
    return atom instanceof Integer ? single((Integer) atom) : (Item) atom;
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
        Node part = !anchor && in(QUANTIFIERS, peek(0)) ? quantify(atom) : atom;
        parts.add(part);
        size = saturate(size + part.size);
      }
      Node node = new Node(Kind.CONCAT, saturate(size));
      node.parts = parts;
      return node;
    }

    private Node atom() {
      int c = take();
      if (c == '(') {
        return group();
      }
      if (c == '[') {
        Node node = new Node(Kind.SET, 1);
        node.set = charClass();
        return node;
      }
      if (c == '\\') {
        return setOf(asItem(escape()));
      }
      if (in(NOT_AN_ATOM, c)) {
        throw new Invalid();
      }
      if (c == '.') {
        return setOf(new Item(new int[] {'\n', '\n'}, true));
      }
      if (c == '^') {
        return new Node(Kind.START, 1);
      }
      if (c == '$') {
        return new Node(Kind.END, 1);
      }
      return setOf(single(c));
    }

    private Node group() {
      Node inner = alternation();
      if (take() != ')') {
        throw new Invalid();
      }
      return inner;
    }

    /** After a backslash: one code point (an Integer), or one of the six shorthand classes. */
    private Object escape() {
      int c = take();
      if (in(ESCAPABLE, c)) {
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
          return new Item(DIGIT.ranges, true);
        case 'W':
          return new Item(WORD.ranges, true);
        case 'S':
          return new Item(SPACE.ranges, true);
        default:
          throw new Invalid();
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
      if (!(from instanceof Integer) || !(to instanceof Integer)) {
        throw new Invalid();
      }
      int low = (Integer) from;
      int high = (Integer) to;
      if (low > high) {
        throw new Invalid();
      }
      return new Item(new int[] {low, high}, false);
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
        int min = c == '+' ? 1 : 0;
        int max = c == '?' ? 1 : -1;
        return repeat(body, min, max, saturate(body.size + 1), false);
      }
      int min = count();
      int max = upper(min);
      if (take() != '}') {
        throw new Invalid();
      }
      if (max < 0) {
        return repeat(body, min, -1, min + 1, true);
      }
      if (min > max) {
        throw new Invalid();
      }
      return repeat(body, min, max, max, true);
    }

    /**
     * After the lower bound of a counted repeat: n again for one bound, -1 for open, m for a range.
     */
    private int upper(int min) {
      if (peek(0) != ',') {
        return min;
      }
      pos++;
      return peek(0) == '}' ? -1 : count();
    }

    private Node repeat(Node body, int min, int max, int sizeOrTimes, boolean byTimes) {
      int size = byTimes ? Math.max(1, saturate(sizeOrTimes * body.size)) : sizeOrTimes;
      Node node = new Node(Kind.REPEAT, size);
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

  /** The tree for a pattern's code points, or {@code null} when it is not a valid pattern. */
  static Node parse(int[] cps) {
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
}
