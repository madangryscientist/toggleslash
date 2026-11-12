"""Patterns for the `matches` op, to the "Patterns" section of docs/evaluation.md.

A hand-written parser reads a pattern, as code points, into a tree that knows each node's size. The tree is
compiled to a small program that a Pike VM runs over code points: no backtracking and no `re`, so a match
costs O(program x input) whatever the pattern. Both the parser and the compiler use an explicit stack, so a
pattern nested 500 deep (it fits the 1024 limit) cannot overflow Python's own.
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass

from ._text import code_points

MAX_LENGTH = 1024
MAX_SIZE = 10000
MAX_COUNT = 100
END = -1

# A character class member: code point ranges as a flat tuple `lo, hi, lo, hi, ...` (inclusive), or their
# complement when `negate` is set.
Item = tuple[tuple[int, ...], bool]
# A class: any of its items, or none of them when `negate` is set.
CharSet = tuple[tuple[Item, ...], bool]


@dataclass(frozen=True, slots=True)
class Node:
    kind: str  # "set", "start", "end", "concat", "alt" or "repeat"
    size: int
    set: CharSet | None = None
    parts: tuple[Node, ...] = ()
    body: Node | None = None
    min: int = 0
    max: int | None = None


def _cp(char: str) -> int:
    return ord(char)


def _single(c: int) -> Item:
    return ((c, c), False)


def _set_of(item: Item) -> Node:
    return Node("set", 1, set=((item,), False))


_DIGIT: Item = ((0x30, 0x39), False)
_WORD: Item = ((0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A), False)
# U+0009 to U+000D are \t \n \v \f \r.
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
_FIXED: dict[int, Node] = {
    _cp("."): _set_of(((_NEWLINE, _NEWLINE), True)),
    _cp("^"): Node("start", 1),
    _cp("$"): Node("end", 1),
}
_NOT_AN_ATOM = frozenset(code_points("*+?{}]"))
_QUANTIFIERS = frozenset(code_points("*+?{"))

_PIPE, _OPEN, _CLOSE = _cp("|"), _cp("("), _cp(")")
_LBRACKET, _RBRACKET, _BACKSLASH = _cp("["), _cp("]"), _cp("\\")
_CARET, _DASH, _LBRACE, _RBRACE, _COMMA = _cp("^"), _cp("-"), _cp("{"), _cp("}"), _cp(",")
_PLUS, _QUESTION, _ZERO = _cp("+"), _cp("?"), _cp("0")


class _Invalid(Exception):
    """The pattern is not valid."""


def _fail() -> _Invalid:
    return _Invalid()


def _saturate(size: int) -> int:
    return min(size, MAX_SIZE + 1)


def _is_digit(c: int) -> bool:
    return 0x30 <= c <= 0x39


def _as_item(atom: int | Item) -> Item:
    return _single(atom) if isinstance(atom, int) else atom


def _concat(parts: list[Node]) -> Node:
    size = sum(part.size for part in parts)
    return Node("concat", _saturate(size), parts=tuple(parts))


def _alternation(sides: list[Node]) -> Node:
    if len(sides) == 1:
        return sides[0]
    size = sum(side.size for side in sides) + len(sides) - 1
    return Node("alt", _saturate(size), parts=tuple(sides))


class _Parser:
    def __init__(self, cps: list[int]) -> None:
        self.cps = cps
        self.pos = 0

    def peek(self, offset: int = 0) -> int:
        at = self.pos + offset
        return self.cps[at] if at < len(self.cps) else END

    def take(self) -> int:
        c = self.peek()
        self.pos += 1
        return c

    def parse(self) -> Node:
        stack: list[tuple[list[Node], list[Node]]] = []
        sides: list[Node] = []
        parts: list[Node] = []
        while True:
            c = self.peek()
            if c == _PIPE:
                self.pos += 1
                sides.append(_concat(parts))
                parts = []
                continue
            if c in (END, _CLOSE):
                sides.append(_concat(parts))
                node = _alternation(sides)
                if c == END:
                    if stack:
                        raise _fail()  # a `(` with no `)`
                    return node
                if not stack:
                    raise _fail()  # an unmatched `)`
                self.pos += 1
                sides, parts = stack.pop()
                self.append(parts, node)
                continue
            self.pos += 1
            if c == _OPEN:
                stack.append((sides, parts))
                sides, parts = [], []
                continue
            self.append(parts, self.atom(c))

    def append(self, parts: list[Node], atom: Node) -> None:
        anchor = atom.kind in ("start", "end")
        parts.append(self.quantify(atom) if not anchor and self.peek() in _QUANTIFIERS else atom)

    def atom(self, c: int) -> Node:
        if c == _LBRACKET:
            return Node("set", 1, set=self.char_class())
        if c == _BACKSLASH:
            return _set_of(_as_item(self.escape()))
        if c in _NOT_AN_ATOM:
            raise _fail()
        return _FIXED.get(c) or _set_of(_single(c))

    def escape(self) -> int | Item:
        """After a `\\`: one code point, or one of the six shorthand classes."""
        c = self.take()
        if c in _ESCAPABLE:
            return c
        found = _ESCAPES.get(c)
        if found is None:
            raise _fail()
        return found

    def char_class(self) -> CharSet:
        """After `[`: the members up to `]`."""
        negate = self.peek() == _CARET
        if negate:
            self.pos += 1
        items = [self.member(True)]
        while self.peek() != _RBRACKET:
            items.append(self.member(False))
        self.pos += 1
        return tuple(items), negate

    def member(self, first: bool) -> Item:
        start = self.class_atom(first)
        is_range = self.peek() == _DASH and self.peek(1) != END and self.peek(1) != _RBRACKET
        if not is_range:
            return _as_item(start)
        self.pos += 1
        end = self.class_atom(False)
        if not isinstance(start, int) or not isinstance(end, int) or start > end:
            raise _fail()
        return ((start, end), False)

    def class_atom(self, first: bool) -> int | Item:
        c = self.take()
        if c in (END, _LBRACKET, _RBRACKET):
            raise _fail()
        if c == _BACKSLASH:
            return self.escape()
        if c == _DASH and not first and self.peek() != _RBRACKET:
            raise _fail()
        return c

    def quantify(self, body: Node) -> Node:
        c = self.take()
        if c != _LBRACE:
            low = 1 if c == _PLUS else 0
            high = 1 if c == _QUESTION else None
            return Node("repeat", _saturate(body.size + 1), body=body, min=low, max=high)
        low = self.count()
        high = self.upper(low)
        if self.take() != _RBRACE:
            raise _fail()
        if high is None:
            return self.repeat(body, low, None, low + 1)
        if low > high:
            raise _fail()
        return self.repeat(body, low, high, high)

    def upper(self, low: int) -> int | None:
        """After `{n`: n again for `{n}`, None for `{n,}`, m for `{n,m}`. Leaves the `}`."""
        if self.peek() != _COMMA:
            return low
        self.pos += 1
        return None if self.peek() == _RBRACE else self.count()

    def repeat(self, body: Node, low: int, high: int | None, times: int) -> Node:
        size = max(1, _saturate(times * body.size))
        return Node("repeat", size, body=body, min=low, max=high)

    def count(self) -> int:
        """ASCII digits with no leading zero, at most 100."""
        c = self.take()
        if not _is_digit(c):
            raise _fail()
        if c == _ZERO:
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
    """The tree for a pattern's code points, or None when it is too long, malformed or too big."""
    if len(cps) > MAX_LENGTH:
        return None
    try:
        node = _Parser(cps).parse()
    except _Invalid:
        return None
    return None if node.size > MAX_SIZE else node


# Instructions are lists so that a jump can be patched: [op, x, y] with op one of the constants below. A
# char instruction keeps its class in x. A jump is a split whose two targets are the same.
_CHAR, _SPLIT, _START, _END, _MATCH = 0, 1, 2, 3, 4

Program = list[list]


def _split(program: Program, x: int) -> list:
    inst = [_SPLIT, x, x]
    program.append(inst)
    return inst


def _emit(root: Node, program: Program) -> None:
    work: list[Node | Callable[[], None]] = [root]
    while work:
        item = work.pop()
        if callable(item):
            item()
            continue
        node = item
        if node.kind == "set":
            program.append([_CHAR, node.set, 0])
        elif node.kind == "start":
            program.append([_START, 0, 0])
        elif node.kind == "end":
            program.append([_END, 0, 0])
        elif node.kind == "concat":
            work.extend(reversed(node.parts))
        elif node.kind == "alt":
            work.extend(reversed(_alt_steps(node.parts, program)))
        else:
            work.extend(reversed(_repeat_steps(node, program)))


def _alt_steps(sides: tuple[Node, ...], program: Program) -> list[Node | Callable[[], None]]:
    jumps: list[list] = []
    steps: list[Node | Callable[[], None]] = []
    last = len(sides) - 1
    for i, side in enumerate(sides):
        if i == last:
            steps.append(side)
            continue
        choice: list[list] = []

        def open_choice(choice: list[list] = choice) -> None:
            choice.append(_split(program, len(program) + 1))

        def close_choice(choice: list[list] = choice) -> None:
            jumps.append(_split(program, 0))
            choice[0][2] = len(program)

        steps.extend((open_choice, side, close_choice))

    def finish() -> None:
        for jump in jumps:
            jump[1] = jump[2] = len(program)

    steps.append(finish)
    return steps


def _repeat_steps(node: Node, program: Program) -> list[Node | Callable[[], None]]:
    body = node.body
    if body is None:  # a repeat node always has a body
        return []
    # A body of size 0 is empty groups only and matches just the empty string, so repeating it emits
    # nothing. Its copies would otherwise cost instructions that the size does not count.
    if body.size == 0:
        return []
    steps: list[Node | Callable[[], None]] = [body] * node.min
    if node.max is None:
        top: list[int] = []
        loop: list[list] = []

        def open_loop() -> None:
            top.append(len(program))
            loop.append(_split(program, len(program) + 1))

        def close_loop() -> None:
            _split(program, top[0])
            loop[0][2] = len(program)

        steps.extend((open_loop, body, close_loop))
        return steps
    optional: list[list] = []

    def open_optional() -> None:
        optional.append(_split(program, len(program) + 1))

    for _ in range(node.min, node.max):
        steps.extend((open_optional, body))

    def finish() -> None:
        for choice in optional:
            choice[2] = len(program)

    steps.append(finish)
    return steps


Pattern = list[list]


def compile_pattern(pattern: str) -> Pattern | None:
    """The compiled pattern, or None when `pattern` is not a valid pattern."""
    node = parse_pattern(code_points(pattern))
    if node is None:
        return None
    program: Program = []
    _emit(node, program)
    program.append([_MATCH, 0, 0])
    return program


def _in_item(item: Item, c: int) -> bool:
    ranges, negate = item
    hit = False
    for i in range(0, len(ranges) - 1, 2):
        if ranges[i] <= c <= ranges[i + 1]:
            hit = True
            break
    return hit != negate


def _in_set(char_set: CharSet, c: int) -> bool:
    items, negate = char_set
    return any(_in_item(item, c) for item in items) != negate


def pattern_matches(program: Pattern, text: str) -> bool:
    """Does the pattern match somewhere in `text`? Each instruction is visited once per position."""
    chars = code_points(text)
    length = len(chars)
    seen = [-1] * len(program)

    def add(found: list[int], pc: int, pos: int) -> bool:
        """Adds `pc` and all it reaches without reading to `found`; True when that reaches a match."""
        stack = [pc]
        while stack:
            at = stack.pop()
            if seen[at] == pos:
                continue
            seen[at] = pos
            op, x, y = program[at]
            if op == _MATCH:
                return True
            if op == _CHAR:
                found.append(at)
            elif op == _SPLIT:
                stack.append(y)
                stack.append(x)
            elif (op == _START and pos == 0) or (op == _END and pos == length):
                stack.append(at + 1)
        return False

    current: list[int] = []
    following: list[int] = []
    pos = 0
    while True:
        if add(current, 0, pos):
            return True
        if pos >= length:
            return False
        c = chars[pos]
        following.clear()
        for pc in current:
            inst = program[pc]
            if inst[0] == _CHAR and _in_set(inst[1], c) and add(following, pc + 1, pos + 1):
                return True
        current, following = following, current
        pos += 1
