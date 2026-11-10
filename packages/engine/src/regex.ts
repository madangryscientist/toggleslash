/**
 * Patterns for the `matches` op, to the "Patterns" section of `docs/evaluation.md`. `pattern-parse.ts`
 * reads a pattern into a tree; this compiles the tree to a small program and runs it on a Pike VM over
 * code points: no backtracking and no `RegExp`, so a match costs O(program x input) whatever the
 * pattern, and it reads the same way as the C# one.
 */

import { type CharSet, type Item, type Node, codePoints, parsePattern } from "./pattern-parse.ts";

/** A jump is a split whose two targets are the same. */
type Inst =
  | { readonly op: "char"; readonly set: CharSet }
  | { readonly op: "split"; x: number; y: number }
  | { readonly op: "start" | "end" | "match" };

type Split = Extract<Inst, { op: "split" }>;

/** A compiled pattern. */
type Pattern = readonly Inst[];

function split(program: Inst[], x: number): Split {
  const inst: Split = { op: "split", x, y: x };
  program.push(inst);
  return inst;
}

function emitAlt(sides: readonly Node[], program: Inst[]): void {
  const jumps: Split[] = [];
  const last = sides.length - 1;
  sides.forEach((side, i) => {
    const choice = i < last ? split(program, program.length + 1) : null;
    emit(side, program);
    if (choice === null) return;
    jumps.push(split(program, 0));
    choice.y = program.length;
  });
  for (const jump of jumps) jump.x = jump.y = program.length;
}

function emitRepeat(body: Node, min: number, max: number | null, program: Inst[]): void {
  // A body of size 0 is empty groups only and matches just the empty string, so repeating it
  // emits nothing. Its copies would otherwise cost instructions that the size does not count.
  if (body.size === 0) return;
  for (let i = 0; i < min; i++) emit(body, program);
  if (max === null) {
    const top = program.length;
    const loop = split(program, top + 1);
    emit(body, program);
    split(program, top);
    loop.y = program.length;
    return;
  }
  const optional: Split[] = [];
  for (let i = min; i < max; i++) {
    optional.push(split(program, program.length + 1));
    emit(body, program);
  }
  for (const choice of optional) choice.y = program.length;
}

function emit(node: Node, program: Inst[]): void {
  switch (node.kind) {
    case "set":
      program.push({ op: "char", set: node.set });
      return;
    case "start":
    case "end":
      program.push({ op: node.kind });
      return;
    case "concat":
      for (const part of node.parts) emit(part, program);
      return;
    case "alt":
      emitAlt(node.sides, program);
      return;
    case "repeat":
      emitRepeat(node.body, node.min, node.max, program);
  }
}

/** The compiled pattern, or null when `pattern` is not a valid pattern. */
export function compilePattern(pattern: string): Pattern | null {
  const node = parsePattern(codePoints(pattern));
  if (node === null) return null;
  const program: Inst[] = [];
  emit(node, program);
  program.push({ op: "match" });
  return program;
}

function inItem(item: Item, c: number): boolean {
  let hit = false;
  for (let i = 0; i + 1 < item.ranges.length && !hit; i += 2) {
    hit = (item.ranges[i] ?? 0) <= c && c <= (item.ranges[i + 1] ?? -1);
  }
  return hit !== item.negate;
}

function inSet(set: CharSet, c: number): boolean {
  return set.items.some((item) => inItem(item, c)) !== set.negate;
}

/** Does the pattern match somewhere in `text`? A Pike VM: each instruction is visited once per position. */
export function patternMatches(program: Pattern, text: string): boolean {
  const input = codePoints(text);
  const length = input.length;
  const seen = new Int32Array(program.length).fill(-1);
  const stack: number[] = [];
  let current: number[] = [];
  let next: number[] = [];

  /** Can an anchor at `op` pass at `pos`? */
  const holds = (op: "start" | "end", pos: number): boolean =>
    pos === (op === "start" ? 0 : length);

  /** Adds `pc` and everything it reaches without reading to `list`; true when that reaches a match. */
  const add = (list: number[], pc: number, pos: number): boolean => {
    stack.push(pc);
    for (let at = stack.pop(); at !== undefined; at = stack.pop()) {
      const inst = program[at];
      if (inst === undefined || seen[at] === pos) continue;
      seen[at] = pos;
      if (inst.op === "match") {
        stack.length = 0;
        return true;
      }
      if (inst.op === "char") list.push(at);
      else if (inst.op === "split") stack.push(inst.y, inst.x);
      else if (holds(inst.op, pos)) stack.push(at + 1);
    }
    return false;
  };

  for (let pos = 0; ; pos++) {
    if (add(current, 0, pos)) return true;
    const c = input[pos];
    if (c === undefined) return false;
    next.length = 0;
    for (const pc of current) {
      const inst = program[pc];
      if (inst?.op === "char" && inSet(inst.set, c) && add(next, pc + 1, pos + 1)) return true;
    }
    [current, next] = [next, current];
  }
}
