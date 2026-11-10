package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * The parser for {@code matches} patterns, to the "Patterns" section of {@code docs/evaluation.md}:
 * code points in, a tree with each node's size out, or {@code null} for a pattern that is not
 * valid. {@link Pattern} compiles the tree and runs it. No platform regular expression is involved.
 */
final class PatternParser {
  private static final int MAX_LENGTH = 1024;
  private static final int MAX_SIZE = 10000;
  private static final int MAX_COUNT = 100;
  private static final int END = -1;
  private static final int NEWLINE = 0x0a;

  /** Code point ranges as pairs {@code lo, hi}, inclusive, or their complement when negated. */
  record Item(int[] ranges, boolean negate) {}

  /** A class: any of its items, or none of them when {@code negate} is set. */
  record CharSet(List<Item> items, boolean negate) {}

  /** A node of the tree. {@code size} is what the spec's size limit counts. */
  abstract static sealed class Node {
    final int size;

    Node(int size) {
      this.size = size;
    }
  }

  static final class SetNode extends Node {
    final CharSet set;

    SetNode(CharSet set) {
      super(1);
      this.set = set;
    }
  }

  static final class AnchorNode extends Node {
    final boolean start;

    AnchorNode(boolean start) {
      super(1);
      this.start = start;
    }
  }

  static final class ConcatNode extends Node {
    final List<Node> parts;

    ConcatNode(List<Node> parts, int size) {
      super(size);
      this.parts = parts;
    }
  }

  static final class AltNode extends Node {
    final List<Node> sides;

    AltNode(List<Node> sides, int size) {
      super(size);
      this.sides = sides;
    }
  }

  static final class RepeatNode extends Node {
    final Node body;
    final int min;

    /** The upper bound, or -1 for none. */
    final int max;

    RepeatNode(Node body, int min, int max, int size) {
      super(size);
      this.body = body;
      this.min = min;
      this.max = max;
    }
  }

  private static final Item DIGIT = new Item(new int[] {0x30, 0x39}, false);
  private static final Item WORD =
      new Item(new int[] {0x30, 0x39, 0x41, 0x5a, 0x5f, 0x5f, 0x61, 0x7a}, false);
  // U+0009 to U+000D are \t \n \v \f \r.
  private static final Item SPACE = new Item(new int[] {0x09, 0x0d, 0x20, 0x20}, false);

  /** {@code \} before one of these is that character. */
  private static final String ESCAPABLE = "\\.[]()|*+?{}^$-";

  /** Code points that cannot start an atom outside a class. */
  private static final String NOT_AN_ATOM = "*+?{}]";

  private static final String QUANTIFIERS = "*+?{";

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

  /** The code points of a UTF-16 string: a valid surrogate pair is one, a lone surrogate itself. */
  static int[] codePoints(String text) {
    int[] points = new int[text.length()];
    int n = 0;
    int i = 0;
    while (i < text.length()) {
      int cp = text.codePointAt(i);
      points[n++] = cp;
      i += Character.charCount(cp);
    }
    return java.util.Arrays.copyOf(points, n);
  }

  /**
   * The tree for a pattern's code points, or null when it is not valid: too long, malformed or big.
   */
  static Node parse(int[] cps) {
    if (cps.length > MAX_LENGTH) {
      return null;
    }
    try {
      PatternParser parser = new PatternParser(cps);
      Node node = parser.alternation();
      if (parser.pos < cps.length) {
        return null; // an unmatched ")"
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

  private int peek() {
    return peek(0);
  }

  private int take() {
    int c = peek();
    pos++;
    return c;
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
    int size = sides.size() - 1;
    for (Node side : sides) {
      size += side.size;
    }
    return new AltNode(sides, saturate(size));
  }

  private Node concat() {
    List<Node> parts = new ArrayList<>();
    int size = 0;
    for (int c = peek(); c != END && c != '|' && c != ')'; c = peek()) {
      Node atom = atom();
      boolean anchor = atom instanceof AnchorNode;
      Node part = !anchor && in(QUANTIFIERS, peek()) ? quantify(atom) : atom;
      parts.add(part);
      size += part.size;
    }
    return new ConcatNode(parts, saturate(size));
  }

  private Node atom() {
    int c = take();
    if (c == '(') {
      return group();
    }
    if (c == '[') {
      return new SetNode(charClass());
    }
    if (c == '\\') {
      return new SetNode(new CharSet(List.of(asItem(escape())), false));
    }
    if (in(NOT_AN_ATOM, c)) {
      throw fail();
    }
    if (c == '.') {
      return new SetNode(new CharSet(List.of(new Item(new int[] {NEWLINE, NEWLINE}, true)), false));
    }
    if (c == '^') {
      return new AnchorNode(true);
    }
    if (c == '$') {
      return new AnchorNode(false);
    }
    return new SetNode(new CharSet(List.of(single(c)), false));
  }

  private Node group() {
    Node inner = alternation();
    if (take() != ')') {
      throw fail();
    }
    return inner;
  }

  /**
   * After a backslash: one code point (an Integer), or one of the six shorthand classes (an Item).
   */
  private Object escape() {
    int c = take();
    if (in(ESCAPABLE, c)) {
      return Integer.valueOf(c);
    }
    switch (c) {
      case 'n':
        return Integer.valueOf(0x0a);
      case 't':
        return Integer.valueOf(0x09);
      case 'r':
        return Integer.valueOf(0x0d);
      case 'f':
        return Integer.valueOf(0x0c);
      case 'v':
        return Integer.valueOf(0x0b);
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

  /** After "[": the members up to "]". */
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

  /** One member: an atom, or a range of two single code points. */
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
    // "]" here is a class with no members; END and "[" are never members.
    if (c == END || c == '[' || c == ']') {
      throw fail();
    }
    if (c == '\\') {
      return escape();
    }
    // An unescaped "-" is a literal only as the first member or directly before "]".
    if (c == '-' && !first && peek() != ']') {
      throw fail();
    }
    return Integer.valueOf(c);
  }

  private Node quantify(Node body) {
    int c = take();
    if (c != '{') {
      int min = c == '+' ? 1 : 0;
      int max = c == '?' ? 1 : -1;
      return new RepeatNode(body, min, max, saturate(body.size + 1));
    }
    int min = count();
    int max = upper(min);
    if (take() != '}') {
      throw fail();
    }
    if (max < 0) {
      return repeat(body, min, -1, min + 1);
    }
    if (min > max) {
      throw fail();
    }
    return repeat(body, min, max, max);
  }

  /** After "{n": n again for "{n}", -1 for "{n,}", m for "{n,m}". Leaves the "}". */
  private int upper(int min) {
    if (peek() != ',') {
      return min;
    }
    pos++;
    return peek() == '}' ? -1 : count();
  }

  private static Node repeat(Node body, int min, int max, int times) {
    return new RepeatNode(body, min, max, Math.max(1, saturate(times * body.size)));
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

  private static Item single(int c) {
    return new Item(new int[] {c, c}, false);
  }

  private static Item asItem(Object atom) {
    return atom instanceof Integer c ? single(c) : (Item) atom;
  }
}
