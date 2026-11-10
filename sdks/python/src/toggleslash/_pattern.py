"""Patterns for the `matches` op, to the "Patterns" section of docs/evaluation.md.

A parser reads a pattern's code points into a tree, the tree compiles to a small program, and a Pike VM runs
the program over code points: no backtracking and no `re`, so a match costs O(program x input) whatever the
pattern. Parsing and compiling use explicit stacks, so a pattern nested hundreds of groups deep does not meet
the interpreter's recursion limit.
"""

from __future__ import annotations

import functools
from collections.abc import Callable
from typing import Any

from ._text import code_points

MAX_LENGTH = 1024
MAX_SIZE = 10000
MAX_COUNT = 100
_END = -1


def _cp(char: str) -> int:
    return ord(char)


# An item is `(ranges, negate)`: code point ranges as `lo, hi` pairs, or their complement.
Item = tuple[tuple[int, ...], bool]
# A class is `(items, negate)`: any of its items, or none of them when `negate` is set.
CharSet = tuple[tuple[Item, ...], bool]

_DIGIT: Item = ((0x30, 0x39), False)
_WORD: Item = ((0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A), False)
_SPACE: Item = ((0x09, 0x0D, 0x20, 0x20), False)
_NEWLINE = 0x0A

_ESCAPABLE = frozenset(code_points("\\.[]()|*+?{}^$-"))
_ESCAPES: dict[int, int | Item] = {
    _cp("n"): 0x0A,
    _cp("t"): 0x09,
    _cp("r"): 0x0D,
    _cp("f"): 0x0C,
    _cp("v"): 0x0B,
    _cp("d"): _DIGIT,
    _cp("w"): _WORD,
    _cp("s"): _SPACE,
    _cp("D"): (_DIGIT[0], True),
    _cp("W"): (_WORD[0], True),
    _cp("S"): (_SPACE[0], True),
}
_NOT_AN_ATOM = frozenset(code_points("*+?{}]"))
_QUANTIFIERS = frozenset(code_points("*+?{"))


class _Node:
    """A parse tree node with its size. `kind` is set, start, end, concat, alt or repeat."""

    __slots__ = ("body", "hi", "kind", "lo", "parts", "set", "size")

    def __init__(self, kind: str, size: int) -> None:
        self.kind = kind
        self.size = size
        self.set: CharSet | None = None
        self.parts: list[_Node] = []
        self.body: _Node | None = None
        self.lo = 0
        self.hi: int | None = None


class _Invalid(Exception):
    pass


def _fail() -> Any:
    raise _Invalid


def _saturate(size: int) -> int:
    return min(size, MAX_SIZE + 1)


def _set_of(item: Item) -> _Node:
    node = _Node("set", 1)
    node.set = ((item,), False)
    return node


def _single(point: int) -> Item:
    return ((point, point), False)


def _as_item(atom: int | Item) -> Item:
    return _single(atom) if isinstance(atom, int) else atom


def _is_digit(point: int) -> bool:
    return 0x30 <= point <= 0x39


def _concat(parts: list[_Node]) -> _Node:
    node = _Node("concat", _saturate(sum(part.size for part in parts)))
    node.parts = parts
    return node


def _alternation(sides: list[_Node]) -> _Node:
    if len(sides) == 1:
        return sides[0]
    node = _Node("alt", _saturate(sum(side.size for side in sides) + len(sides) - 1))
    node.parts = sides
    return node


class _Parser:
    def __init__(self, points: list[int]) -> None:
        self.points = points
        self.pos = 0

    def peek(self, offset: int = 0) -> int:
        at = self.pos + offset
        return self.points[at] if at < len(self.points) else _END

    def take(self) -> int:
        point = self.peek()
        self.pos += 1
        return point

    def parse(self) -> _Node:
        stack: list[tuple[list[_Node], list[_Node]]] = []
        sides: list[_Node] = []
        parts: list[_Node] = []
        while True:
            point = self.peek()
            if point == _END:
                if stack:
                    _fail()  # an unclosed `(`
                break
            if point == _cp("|"):
                self.pos += 1
                sides.append(_concat(parts))
                parts = []
            elif point == _cp(")"):
                if not stack:
                    _fail()  # an unmatched `)`
                self.pos += 1
                sides.append(_concat(parts))
                group = _alternation(sides)
                sides, parts = stack.pop()
                parts.append(self.quantify(group) if self.peek() in _QUANTIFIERS else group)
            elif point == _cp("("):
                self.pos += 1
                stack.append((sides, parts))
                sides, parts = [], []
            else:
                atom = self.atom()
                anchor = atom.kind in ("start", "end")
                parts.append(atom if anchor or self.peek() not in _QUANTIFIERS else self.quantify(atom))
        sides.append(_concat(parts))
        return _alternation(sides)

    def atom(self) -> _Node:
        point = self.take()
        if point == _cp("["):
            node = _Node("set", 1)
            node.set = self.char_class()
            return node
        if point == _cp("\\"):
            return _set_of(_as_item(self.escape()))
        if point in _NOT_AN_ATOM:
            _fail()
        if point == _cp("."):
            return _set_of(((_NEWLINE, _NEWLINE), True))
        if point == _cp("^"):
            return _Node("start", 1)
        if point == _cp("$"):
            return _Node("end", 1)
        return _set_of(_single(point))

    def escape(self) -> int | Item:
        point = self.take()
        if point in _ESCAPABLE:
            return point
        escaped = _ESCAPES.get(point)
        return escaped if escaped is not None else _fail()

    def char_class(self) -> CharSet:
        negate = self.peek() == _cp("^")
        if negate:
            self.pos += 1
        items = [self.member(True)]
        while self.peek() != _cp("]"):
            items.append(self.member(False))
        self.pos += 1
        return tuple(items), negate

    def member(self, first: bool) -> Item:
        start = self.class_atom(first)
        is_range = self.peek() == _cp("-") and self.peek(1) not in (_END, _cp("]"))
        if not is_range:
            return _as_item(start)
        self.pos += 1
        end = self.class_atom(False)
        if not isinstance(start, int) or not isinstance(end, int) or start > end:
            _fail()
        return ((start, end), False)

    def class_atom(self, first: bool) -> int | Item:
        point = self.take()
        if point in (_END, _cp("["), _cp("]")):
            _fail()
        if point == _cp("\\"):
            return self.escape()
        if point == _cp("-") and not first and self.peek() != _cp("]"):
            _fail()
        return point

    def quantify(self, body: _Node) -> _Node:
        point = self.take()
        if point != _cp("{"):
            node = _Node("repeat", _saturate(body.size + 1))
            node.body = body
            node.lo = 1 if point == _cp("+") else 0
            node.hi = 1 if point == _cp("?") else None
            return node
        low = self.count()
        high = self.upper(low)
        if self.take() != _cp("}"):
            _fail()
        if high is None:
            return self.repeat(body, low, None, low + 1)
        if low > high:
            _fail()
        return self.repeat(body, low, high, high)

    def upper(self, low: int) -> int | None:
        if self.peek() != _cp(","):
            return low
        self.pos += 1
        return None if self.peek() == _cp("}") else self.count()

    @staticmethod
    def repeat(body: _Node, low: int, high: int | None, times: int) -> _Node:
        node = _Node("repeat", max(1, _saturate(times * body.size)))
        node.body = body
        node.lo = low
        node.hi = high
        return node

    def count(self) -> int:
        point = self.take()
        if not _is_digit(point):
            _fail()
        if point == _cp("0"):
            return _fail() if _is_digit(self.peek()) else 0
        value = point - 0x30
        while _is_digit(self.peek()):
            value = value * 10 + (self.take() - 0x30)
            if value > MAX_COUNT:
                _fail()
        return value


def parse_pattern(points: list[int]) -> _Node | None:
    """The tree for a pattern's code points, or `None` when it is too long, malformed or too big."""
    if len(points) > MAX_LENGTH:
        return None
    try:
        node = _Parser(points).parse()
    except _Invalid:
        return None
    return None if node.size > MAX_SIZE else node


# A program is a list of instructions, each `[op, a, b]`.
_CHAR, _SPLIT, _START, _END_ANCHOR, _MATCH = range(5)
Program = list[list[Any]]


def _split(program: Program, target: int) -> list[Any]:
    inst: list[Any] = [_SPLIT, target, target]
    program.append(inst)
    return inst


def _plan_alt(node: _Node, program: Program, steps: list[Any]) -> None:
    sides = node.parts
    last = len(sides) - 1
    jumps: list[list[Any]] = []
    for index, side in enumerate(sides):
        choice: list[list[Any]] = []
        if index < last:
            steps.append(lambda choice=choice: choice.append(_split(program, len(program) + 1)))
        steps.append(side)
        if index < last:

            def finish_side(choice: list[list[Any]] = choice) -> None:
                jumps.append(_split(program, 0))
                choice[0][2] = len(program)

            steps.append(finish_side)

    def land() -> None:
        for jump in jumps:
            jump[1] = jump[2] = len(program)

    steps.append(land)


def _plan_repeat(node: _Node, program: Program, steps: list[Any]) -> None:
    body = node.body
    assert body is not None
    # A body of size 0 is empty groups only and matches just the empty string, so repeating it emits nothing.
    if body.size == 0:
        return
    steps.extend([body] * node.lo)
    if node.hi is None:
        loop: list[list[Any]] = []
        top: list[int] = []

        def open_loop() -> None:
            top.append(len(program))
            loop.append(_split(program, top[0] + 1))

        def close_loop() -> None:
            _split(program, top[0])
            loop[0][2] = len(program)

        steps.extend([open_loop, body, close_loop])
        return
    optional: list[list[Any]] = []
    for _ in range(node.lo, node.hi):
        steps.append(lambda: optional.append(_split(program, len(program) + 1)))
        steps.append(body)

    def land() -> None:
        for choice in optional:
            choice[2] = len(program)

    steps.append(land)


def _emit(root: _Node, program: Program) -> None:
    work: list[Any] = [root]
    while work:
        item = work.pop()
        if isinstance(item, _Node):
            if item.kind == "set":
                program.append([_CHAR, item.set, None])
            elif item.kind == "start":
                program.append([_START, None, None])
            elif item.kind == "end":
                program.append([_END_ANCHOR, None, None])
            elif item.kind == "concat":
                work.extend(reversed(item.parts))
            else:
                steps: list[Callable[[], None] | _Node] = []
                if item.kind == "alt":
                    _plan_alt(item, program, steps)
                else:
                    _plan_repeat(item, program, steps)
                work.extend(reversed(steps))
        else:
            item()


@functools.lru_cache(maxsize=512)
def compile_pattern(pattern: str) -> tuple[Any, ...] | None:
    """The compiled program for `pattern` (read-only), or `None` when it is not a valid pattern."""
    node = parse_pattern(code_points(pattern))
    if node is None:
        return None
    program: Program = []
    _emit(node, program)
    program.append([_MATCH, None, None])
    return tuple(tuple(inst) for inst in program)


def _in_item(item: Item, point: int) -> bool:
    ranges, negate = item
    hit = any(ranges[i] <= point <= ranges[i + 1] for i in range(0, len(ranges) - 1, 2))
    return hit != negate


def _in_set(char_set: CharSet, point: int) -> bool:
    items, negate = char_set
    return any(_in_item(item, point) for item in items) != negate


def pattern_matches(program: tuple[Any, ...], text: str) -> bool:
    """Does the pattern match somewhere in `text`? A Pike VM: each instruction is visited once per position."""
    points = code_points(text)
    length = len(points)
    seen = [-1] * len(program)
    stack: list[int] = []

    def add(threads: list[int], pc: int, pos: int) -> bool:
        """Adds `pc` and everything it reaches without reading; true when that reaches a match."""
        stack.append(pc)
        while stack:
            at = stack.pop()
            if seen[at] == pos:
                continue
            seen[at] = pos
            op, first, second = program[at]
            if op == _MATCH:
                stack.clear()
                return True
            if op == _CHAR:
                threads.append(at)
            elif op == _SPLIT:
                stack.append(second)
                stack.append(first)
            elif pos == (0 if op == _START else length):
                stack.append(at + 1)
        return False

    current: list[int] = []
    upcoming: list[int] = []
    pos = 0
    while True:
        if add(current, 0, pos):
            return True
        if pos >= length:
            return False
        point = points[pos]
        upcoming.clear()
        for pc in current:
            inst = program[pc]
            if inst[0] == _CHAR and _in_set(inst[1], point) and add(upcoming, pc + 1, pos + 1):
                return True
        current, upcoming = upcoming, current
        pos += 1
