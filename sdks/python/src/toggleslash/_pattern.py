"""Patterns for the `matches` op, to the "Patterns" section of docs/evaluation.md.

A parser into a tree with each node's size, a compiler to a small program, and a Pike VM over code points: no
backtracking and no `re`, so a match costs O(program x input) whatever the pattern.
"""

from __future__ import annotations

from typing import NamedTuple

from ._text import code_points

MAX_LENGTH = 1024
MAX_SIZE = 10000
MAX_COUNT = 100
END = -1


class Item(NamedTuple):
    ranges: tuple[int, ...]
    negate: bool


class CharSet(NamedTuple):
    items: tuple[Item, ...]
    negate: bool


class Node:
    """kind: set | start | end | concat | alt | repeat."""

    __slots__ = ("body", "kind", "max", "min", "parts", "set", "size")

    def __init__(
        self,
        kind: str,
        size: int,
        set_: CharSet | None = None,
        parts: list[Node] | None = None,
        body: Node | None = None,
        min_: int = 0,
        max_: int | None = None,
    ) -> None:
        self.kind = kind
        self.size = size
        self.set = set_
        self.parts = parts or []
        self.body = body
        self.min = min_
        self.max = max_


def _cp(c: str) -> int:
    return ord(c)


def _single(c: int) -> Item:
    return Item((c, c), False)


def _set_of(item: Item) -> Node:
    return Node("set", 1, set_=CharSet((item,), False))


DIGIT = Item((0x30, 0x39), False)
WORD = Item((0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A), False)
SPACE = Item((0x09, 0x0D, 0x20, 0x20), False)
NEWLINE = 0x0A

ESCAPABLE = frozenset(_cp(c) for c in "\\.[]()|*+?{}^$-")
ESCAPES: dict[int, int | Item] = {
    _cp("n"): 0x0A,
    _cp("t"): 0x09,
    _cp("r"): 0x0D,
    _cp("f"): 0x0C,
    _cp("v"): 0x0B,
    _cp("d"): DIGIT,
    _cp("w"): WORD,
    _cp("s"): SPACE,
    _cp("D"): Item(DIGIT.ranges, True),
    _cp("W"): Item(WORD.ranges, True),
    _cp("S"): Item(SPACE.ranges, True),
}
NOT_AN_ATOM = frozenset(_cp(c) for c in "*+?{}]")
QUANTIFIERS = frozenset(_cp(c) for c in "*+?{")


class _Invalid(Exception):
    pass


def _fail() -> _Invalid:
    return _Invalid()


def _saturate(size: int) -> int:
    return min(size, MAX_SIZE + 1)


def _is_digit(c: int) -> bool:
    return 0x30 <= c <= 0x39


def _as_item(atom: int | Item) -> Item:
    return _single(atom) if isinstance(atom, int) else atom


class _Parser:
    def __init__(self, cps: list[int]) -> None:
        self.cps = cps
        self.pos = 0

    def parse(self) -> Node:
        node = self.alternation()
        if self.pos < len(self.cps):
            raise _fail()
        return node

    def peek(self, offset: int = 0) -> int:
        at = self.pos + offset
        return self.cps[at] if at < len(self.cps) else END

    def take(self) -> int:
        c = self.peek()
        self.pos += 1
        return c

    def alternation(self) -> Node:
        sides = [self.concat()]
        while self.peek() == _cp("|"):
            self.pos += 1
            sides.append(self.concat())
        if len(sides) == 1:
            return sides[0]
        size = sum(s.size for s in sides) + len(sides) - 1
        return Node("alt", _saturate(size), parts=sides)

    def concat(self) -> Node:
        parts: list[Node] = []
        c = self.peek()
        while c != END and c != _cp("|") and c != _cp(")"):
            atom = self.atom()
            anchor = atom.kind in ("start", "end")
            parts.append(self.quantify(atom) if not anchor and self.peek() in QUANTIFIERS else atom)
            c = self.peek()
        return Node("concat", _saturate(sum(p.size for p in parts)), parts=parts)

    def atom(self) -> Node:
        c = self.take()
        if c == _cp("("):
            return self.group()
        if c == _cp("["):
            return Node("set", 1, set_=self.char_class())
        if c == _cp("\\"):
            return _set_of(_as_item(self.escape()))
        if c in NOT_AN_ATOM:
            raise _fail()
        if c == _cp("."):
            return _set_of(Item((NEWLINE, NEWLINE), True))
        if c == _cp("^"):
            return Node("start", 1)
        if c == _cp("$"):
            return Node("end", 1)
        return _set_of(_single(c))

    def group(self) -> Node:
        inner = self.alternation()
        if self.take() != _cp(")"):
            raise _fail()
        return inner

    def escape(self) -> int | Item:
        c = self.take()
        if c in ESCAPABLE:
            return c
        if c in ESCAPES:
            return ESCAPES[c]
        raise _fail()

    def char_class(self) -> CharSet:
        negate = self.peek() == _cp("^")
        if negate:
            self.pos += 1
        items = [self.member(True)]
        while self.peek() != _cp("]"):
            items.append(self.member(False))
        self.pos += 1
        return CharSet(tuple(items), negate)

    def member(self, first: bool) -> Item:
        from_ = self.class_atom(first)
        is_range = self.peek() == _cp("-") and self.peek(1) != END and self.peek(1) != _cp("]")
        if not is_range:
            return _as_item(from_)
        self.pos += 1
        to = self.class_atom(False)
        if not isinstance(from_, int) or not isinstance(to, int) or from_ > to:
            raise _fail()
        return Item((from_, to), False)

    def class_atom(self, first: bool) -> int | Item:
        c = self.take()
        if c == END or c == _cp("[") or c == _cp("]"):
            raise _fail()
        if c == _cp("\\"):
            return self.escape()
        if c == _cp("-") and not first and self.peek() != _cp("]"):
            raise _fail()
        return c

    def quantify(self, body: Node) -> Node:
        c = self.take()
        if c != _cp("{"):
            min_ = 1 if c == _cp("+") else 0
            max_ = 1 if c == _cp("?") else None
            return Node("repeat", _saturate(body.size + 1), body=body, min_=min_, max_=max_)
        min_ = self.count()
        max_ = self.upper(min_)
        if self.take() != _cp("}"):
            raise _fail()
        if max_ is None:
            return self.repeat(body, min_, None, min_ + 1)
        if min_ > max_:
            raise _fail()
        return self.repeat(body, min_, max_, max_)

    def upper(self, min_: int) -> int | None:
        if self.peek() != _cp(","):
            return min_
        self.pos += 1
        return None if self.peek() == _cp("}") else self.count()

    def repeat(self, body: Node, min_: int, max_: int | None, times: int) -> Node:
        size = max(1, _saturate(times * body.size))
        return Node("repeat", size, body=body, min_=min_, max_=max_)

    def count(self) -> int:
        c = self.take()
        if not _is_digit(c):
            raise _fail()
        if c == _cp("0"):
            if _is_digit(self.peek()):
                raise _fail()
            return 0
        value = c - 0x30
        while _is_digit(self.peek()):
            value = value * 10 + (self.take() - 0x30)
            if value > MAX_COUNT:
                raise _fail()
        return value


def parse_pattern(cps: list[int]) -> Node | None:
    if len(cps) > MAX_LENGTH:
        return None
    try:
        node = _Parser(cps).parse()
    except _Invalid:
        return None
    return None if node.size > MAX_SIZE else node


# The program: a list of instructions. Each is a list so a split can be patched after it is emitted.
#   [CHAR, CharSet]   [SPLIT, x, y]   [START]   [END]   [MATCH]
_CHAR, _SPLIT, _START, _END, _MATCH = 0, 1, 2, 3, 4

Program = list[list]


def _split(program: Program, x: int) -> list:
    inst = [_SPLIT, x, x]
    program.append(inst)
    return inst


def _emit_alt(sides: list[Node], program: Program) -> None:
    jumps: list[list] = []
    last = len(sides) - 1
    for i, side in enumerate(sides):
        choice = _split(program, len(program) + 1) if i < last else None
        _emit(side, program)
        if choice is None:
            continue
        jumps.append(_split(program, 0))
        choice[2] = len(program)
    for jump in jumps:
        jump[1] = jump[2] = len(program)


def _emit_repeat(body: Node, min_: int, max_: int | None, program: Program) -> None:
    if body.size == 0:
        return
    for _ in range(min_):
        _emit(body, program)
    if max_ is None:
        top = len(program)
        loop = _split(program, top + 1)
        _emit(body, program)
        _split(program, top)
        loop[2] = len(program)
        return
    optional: list[list] = []
    for _ in range(min_, max_):
        optional.append(_split(program, len(program) + 1))
        _emit(body, program)
    for choice in optional:
        choice[2] = len(program)


def _emit(node: Node, program: Program) -> None:
    kind = node.kind
    if kind == "set":
        program.append([_CHAR, node.set])
    elif kind == "start":
        program.append([_START])
    elif kind == "end":
        program.append([_END])
    elif kind == "concat":
        for part in node.parts:
            _emit(part, program)
    elif kind == "alt":
        _emit_alt(node.parts, program)
    else:
        assert node.body is not None
        _emit_repeat(node.body, node.min, node.max, program)


def compile_pattern(pattern: str) -> Program | None:
    node = parse_pattern(code_points(pattern))
    if node is None:
        return None
    program: Program = []
    _emit(node, program)
    program.append([_MATCH])
    return program


def _in_item(item: Item, c: int) -> bool:
    hit = False
    ranges = item.ranges
    for i in range(0, len(ranges) - 1, 2):
        if ranges[i] <= c <= ranges[i + 1]:
            hit = True
            break
    return hit != item.negate


def _in_set(char_set: CharSet, c: int) -> bool:
    return any(_in_item(item, c) for item in char_set.items) != char_set.negate


def pattern_matches(program: Program, text: str) -> bool:
    """Does the pattern match somewhere in `text`? Each instruction is visited once per position."""
    data = code_points(text)
    length = len(data)
    seen = [-1] * len(program)
    stack: list[int] = []

    def add(lst: list[int], pc: int, pos: int) -> bool:
        stack.append(pc)
        while stack:
            at = stack.pop()
            inst = program[at]
            if seen[at] == pos:
                continue
            seen[at] = pos
            op = inst[0]
            if op == _MATCH:
                stack.clear()
                return True
            if op == _CHAR:
                lst.append(at)
            elif op == _SPLIT:
                stack.append(inst[2])
                stack.append(inst[1])
            elif (pos == 0) if op == _START else (pos == length):
                stack.append(at + 1)
        return False

    current: list[int] = []
    nxt: list[int] = []
    pos = 0
    while True:
        if add(current, 0, pos):
            return True
        if pos >= length:
            return False
        c = data[pos]
        nxt.clear()
        for pc in current:
            inst = program[pc]
            if inst[0] == _CHAR and _in_set(inst[1], c) and add(nxt, pc + 1, pos + 1):
                return True
        current, nxt = nxt, current
        pos += 1
