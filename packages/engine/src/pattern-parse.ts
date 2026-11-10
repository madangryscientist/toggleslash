/**
 * The parser for `matches` patterns, to the "Patterns" section of `docs/evaluation.md`: code points in,
 * a tree with each node's size out, or null for a pattern that is not valid. `regex.ts` compiles the
 * tree and runs it.
 */

const MAX_LENGTH = 1024;
const MAX_SIZE = 10000;
const MAX_COUNT = 100;
const END = -1;

/** Code point ranges as pairs `lo, hi`, inclusive, or their complement when `negate` is set. */
export interface Item {
  readonly ranges: readonly number[];
  readonly negate: boolean;
}

/** A class: any of its items, or none of them when `negate` is set. */
export interface CharSet {
  readonly items: readonly Item[];
  readonly negate: boolean;
}

export type Node =
  | { readonly kind: "set"; readonly set: CharSet; readonly size: number }
  | { readonly kind: "start" | "end"; readonly size: number }
  | { readonly kind: "concat"; readonly parts: readonly Node[]; readonly size: number }
  | { readonly kind: "alt"; readonly sides: readonly Node[]; readonly size: number }
  | {
      readonly kind: "repeat";
      readonly body: Node;
      readonly min: number;
      readonly max: number | null;
      readonly size: number;
    };

/** The code points of a UTF-16 string: a valid surrogate pair is one, a lone surrogate is itself. */
export function codePoints(text: string): number[] {
  const points: number[] = [];
  for (let i = 0; i < text.length; i++) {
    const high = text.charCodeAt(i);
    const low = i + 1 < text.length ? text.charCodeAt(i + 1) : 0;
    const pair = high >= 0xd800 && high <= 0xdbff && low >= 0xdc00 && low <= 0xdfff;
    points.push(pair ? ((high - 0xd800) << 10) + (low - 0xdc00) + 0x10000 : high);
    if (pair) i++;
  }
  return points;
}

const cp = (c: string): number => c.charCodeAt(0);

const single = (c: number): Item => ({ ranges: [c, c], negate: false });
const setOf = (item: Item): Node => ({
  kind: "set",
  set: { items: [item], negate: false },
  size: 1,
});

const DIGIT: Item = { ranges: [0x30, 0x39], negate: false };
const WORD: Item = { ranges: [0x30, 0x39, 0x41, 0x5a, 0x5f, 0x5f, 0x61, 0x7a], negate: false };
// U+0009 to U+000D are \t \n \v \f \r.
const SPACE: Item = { ranges: [0x09, 0x0d, 0x20, 0x20], negate: false };
const NEWLINE = 0x0a;

/** `\` before one of these is that character. */
const ESCAPABLE: ReadonlySet<number> = new Set(codePoints("\\.[]()|*+?{}^$-"));
const ESCAPES: ReadonlyMap<number, number | Item> = new Map<number, number | Item>([
  [cp("n"), 0x0a],
  [cp("t"), 0x09],
  [cp("r"), 0x0d],
  [cp("f"), 0x0c],
  [cp("v"), 0x0b],
  [cp("d"), DIGIT],
  [cp("w"), WORD],
  [cp("s"), SPACE],
  [cp("D"), { ...DIGIT, negate: true }],
  [cp("W"), { ...WORD, negate: true }],
  [cp("S"), { ...SPACE, negate: true }],
]);

/** Atoms that are one fixed node. */
const FIXED: ReadonlyMap<number, Node> = new Map<number, Node>([
  [cp("."), setOf({ ranges: [NEWLINE, NEWLINE], negate: true })],
  [cp("^"), { kind: "start", size: 1 }],
  [cp("$"), { kind: "end", size: 1 }],
]);

/** Code points that cannot start an atom outside a class. */
const NOT_AN_ATOM: ReadonlySet<number> = new Set(codePoints("*+?{}]"));
const QUANTIFIERS: ReadonlySet<number> = new Set(codePoints("*+?{"));

class Invalid extends Error {}

function fail(): never {
  throw new Invalid();
}

const saturate = (size: number): number => Math.min(size, MAX_SIZE + 1);

const isDigit = (c: number): boolean => c >= 0x30 && c <= 0x39;

class Parser {
  private pos = 0;

  constructor(private readonly cps: readonly number[]) {}

  parse(): Node {
    const node = this.alternation();
    if (this.pos < this.cps.length) fail(); // an unmatched `)`
    return node;
  }

  private peek(offset = 0): number {
    return this.cps[this.pos + offset] ?? END;
  }

  private take(): number {
    const c = this.peek();
    this.pos++;
    return c;
  }

  private alternation(): Node {
    const sides = [this.concat()];
    while (this.peek() === cp("|")) {
      this.pos++;
      sides.push(this.concat());
    }
    const [only] = sides;
    if (sides.length === 1 && only !== undefined) return only;
    const size = sides.reduce((sum, side) => sum + side.size, sides.length - 1);
    return { kind: "alt", sides, size: saturate(size) };
  }

  private concat(): Node {
    const parts: Node[] = [];
    for (let c = this.peek(); c !== END && c !== cp("|") && c !== cp(")"); c = this.peek()) {
      const atom = this.atom();
      const anchor = atom.kind === "start" || atom.kind === "end";
      parts.push(!anchor && QUANTIFIERS.has(this.peek()) ? this.quantify(atom) : atom);
    }
    const size = parts.reduce((sum, part) => sum + part.size, 0);
    return { kind: "concat", parts, size: saturate(size) };
  }

  private atom(): Node {
    const c = this.take();
    if (c === cp("(")) return this.group();
    if (c === cp("[")) return { kind: "set", set: this.charClass(), size: 1 };
    if (c === cp("\\")) return setOf(asItem(this.escape()));
    if (NOT_AN_ATOM.has(c)) fail();
    return FIXED.get(c) ?? setOf(single(c));
  }

  private group(): Node {
    const inner = this.alternation();
    if (this.take() !== cp(")")) fail();
    return inner;
  }

  /** After a `\`: one code point, or one of the six shorthand classes. */
  private escape(): number | Item {
    const c = this.take();
    if (ESCAPABLE.has(c)) return c;
    return ESCAPES.get(c) ?? fail();
  }

  /** After `[`: the members up to `]`. */
  private charClass(): CharSet {
    const negate = this.peek() === cp("^");
    if (negate) this.pos++;
    const items: Item[] = [this.member(true)];
    while (this.peek() !== cp("]")) items.push(this.member(false));
    this.pos++;
    return { items, negate };
  }

  /** One member: an atom, or a range of two single code points. */
  private member(first: boolean): Item {
    const from = this.classAtom(first);
    const range = this.peek() === cp("-") && this.peek(1) !== END && this.peek(1) !== cp("]");
    if (!range) return asItem(from);
    this.pos++;
    const to = this.classAtom(false);
    if (typeof from !== "number" || typeof to !== "number" || from > to) fail();
    return { ranges: [from, to], negate: false };
  }

  private classAtom(first: boolean): number | Item {
    const c = this.take();
    // `]` here is a class with no members; END and `[` are never members.
    if (c === END || c === cp("[") || c === cp("]")) fail();
    if (c === cp("\\")) return this.escape();
    // An unescaped `-` is a literal only as the first member or directly before `]`.
    if (c === cp("-") && !first && this.peek() !== cp("]")) fail();
    return c;
  }

  private quantify(body: Node): Node {
    const c = this.take();
    if (c !== cp("{")) {
      const min = c === cp("+") ? 1 : 0;
      const max = c === cp("?") ? 1 : null;
      return { kind: "repeat", body, min, max, size: saturate(body.size + 1) };
    }
    const min = this.count();
    const max = this.upper(min);
    if (this.take() !== cp("}")) fail();
    if (max === null) return this.repeat(body, min, null, min + 1);
    if (min > max) fail();
    return this.repeat(body, min, max, max);
  }

  /** After `{n`: n again for `{n}`, null for `{n,}`, m for `{n,m}`. Leaves the `}`. */
  private upper(min: number): number | null {
    if (this.peek() !== cp(",")) return min;
    this.pos++;
    return this.peek() === cp("}") ? null : this.count();
  }

  private repeat(body: Node, min: number, max: number | null, times: number): Node {
    return { kind: "repeat", body, min, max, size: Math.max(1, saturate(times * body.size)) };
  }

  /** ASCII digits with no leading zero, at most 100. */
  private count(): number {
    const c = this.take();
    if (!isDigit(c)) fail();
    if (c === cp("0")) return isDigit(this.peek()) ? fail() : 0;
    let value = c - 0x30;
    while (isDigit(this.peek())) {
      value = value * 10 + (this.take() - 0x30);
      if (value > MAX_COUNT) fail();
    }
    return value;
  }
}

function asItem(atom: number | Item): Item {
  return typeof atom === "number" ? single(atom) : atom;
}

/** The tree for a pattern's code points, or null when it is not a valid pattern: too long, malformed or too big. */
export function parsePattern(cps: readonly number[]): Node | null {
  if (cps.length > MAX_LENGTH) return null;
  try {
    const node = new Parser(cps).parse();
    return node.size > MAX_SIZE ? null : node;
  } catch (error) {
    if (error instanceof Invalid) return null;
    throw error;
  }
}
