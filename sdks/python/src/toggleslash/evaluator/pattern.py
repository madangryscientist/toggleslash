"""Patterns for the ``matches`` op, to "Patterns" in docs/evaluation.md.

The parser reads a pattern's code points into a tree with each node's size; the compiler turns the
tree into a small program; a Pike VM runs it over code points. There is no backtracking and no
``re``, so a match costs O(program x input) whatever the pattern.
"""

from __future__ import annotations

from dataclasses import dataclass

from .text import code_points

MAX_LENGTH = 1024
MAX_SIZE = 10000
MAX_COUNT = 100
END = -1


@dataclass(frozen=True, slots=True)
class Item:
    """Code point ranges as pairs ``lo, hi`` (inclusive), or their complement when ``negate``."""

    ranges: tuple[int, ...]
    negate: bool = False


@dataclass(frozen=True, slots=True)
class CharSet:
    """A class: any of its items, or none of them when ``negate`` is set."""

    items: tuple[Item, ...]
    negate: bool = False


@dataclass(frozen=True, slots=True)
class Node:
    kind: str  # set, start, end, concat, alt, repeat
    size: int
    charset: CharSet | None = None
    parts: tuple[Node, ...] = ()
    body: Node | None = None
    minimum: int = 0
    maximum: int | None = None


def _cp(char: str) -> int:
    return ord(char)


_DIGIT = Item((0x30, 0x39))
_WORD = Item((0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A))
_SPACE = Item((0x09, 0x0D, 0x20, 0x20))
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
    _cp("D"): Item(_DIGIT.ranges, True),
    _cp("W"): Item(_WORD.ranges, True),
    _cp("S"): Item(_SPACE.ranges, True),
}
_NOT_AN_ATOM = frozenset(code_points("*+?{}]"))
_QUANTIFIERS = frozenset(code_points("*+?{"))


class _Invalid(Exception):
    pass


def _fail() -> _Invalid:
    return _Invalid()


def _saturate(size: int) -> int:
    return min(size, MAX_SIZE + 1)


def _is_digit(point: int) -> bool:
    return 0x30 <= point <= 0x39


def _single(point: int) -> Item:
    return Item((point, point))


def _set_of(item: Item) -> Node:
    return Node("set", 1, charset=CharSet((item,)))


def _as_item(atom: int | Item) -> Item:
    return _single(atom) if isinstance(atom, int) else atom


_ANY = _set_of(Item((_NEWLINE, _NEWLINE), True))
_START = Node("start", 1)
_END = Node("end", 1)


class _Parser:
    def __init__(self, points: list[int]) -> None:
        self.points = points
        self.pos = 0

    def parse(self) -> Node:
        node = self.alternation()
        if self.pos < len(self.points):
            raise _fail()  # an unmatched `)`
        return node

    def peek(self, offset: int = 0) -> int:
        at = self.pos + offset
        return self.points[at] if at < len(self.points) else END

    def take(self) -> int:
        point = self.peek()
        self.pos += 1
        return point

    def alternation(self) -> Node:
        sides = [self.concat()]
        while self.peek() == _cp("|"):
            self.pos += 1
            sides.append(self.concat())
        if len(sides) == 1:
            return sides[0]
        size = sum(side.size for side in sides) + len(sides) - 1
        return Node("alt", _saturate(size), parts=tuple(sides))

    def concat(self) -> Node:
        parts: list[Node] = []
        while self.peek() not in (END, _cp("|"), _cp(")")):
            atom = self.atom()
            anchor = atom.kind in ("start", "end")
            parts.append(self.quantify(atom) if not anchor and self.peek() in _QUANTIFIERS else atom)
        size = sum(part.size for part in parts)
        return Node("concat", _saturate(size), parts=tuple(parts))

    def atom(self) -> Node:
        point = self.take()
        if point == _cp("("):
            return self.group()
        if point == _cp("["):
            return Node("set", 1, charset=self.char_class())
        if point == _cp("\\"):
            return _set_of(_as_item(self.escape()))
        if point in _NOT_AN_ATOM or point == END:
            raise _fail()
        if point == _cp("."):
            return _ANY
        if point == _cp("^"):
            return _START
        if point == _cp("$"):
            return _END
        return _set_of(_single(point))

    def group(self) -> Node:
        inner = self.alternation()
        if self.take() != _cp(")"):
            raise _fail()
        return inner

    def escape(self) -> int | Item:
        point = self.take()
        if point in _ESCAPABLE:
            return point
        found = _ESCAPES.get(point)
        if found is None:
            raise _fail()
        return found

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
        start = self.class_atom(first)
        is_range = self.peek() == _cp("-") and self.peek(1) not in (END, _cp("]"))
        if not is_range:
            return _as_item(start)
        self.pos += 1
        stop = self.class_atom(False)
        if not isinstance(start, int) or not isinstance(stop, int) or start > stop:
            raise _fail()
        return Item((start, stop))

    def class_atom(self, first: bool) -> int | Item:
        point = self.take()
        if point in (END, _cp("["), _cp("]")):
            raise _fail()
        if point == _cp("\\"):
            return self.escape()
        if point == _cp("-") and not first and self.peek() != _cp("]"):
            raise _fail()
        return point

    def quantify(self, body: Node) -> Node:
        point = self.take()
        if point != _cp("{"):
            minimum = 1 if point == _cp("+") else 0
            maximum = 1 if point == _cp("?") else None
            return Node("repeat", _saturate(body.size + 1), body=body, minimum=minimum, maximum=maximum)
        minimum = self.count()
        maximum = self.upper(minimum)
        if self.take() != _cp("}"):
            raise _fail()
        if maximum is None:
            return self.repeat(body, minimum, None, minimum + 1)
        if minimum > maximum:
            raise _fail()
        return self.repeat(body, minimum, maximum, maximum)

    def upper(self, minimum: int) -> int | None:
        if self.peek() != _cp(","):
            return minimum
        self.pos += 1
        return None if self.peek() == _cp("}") else self.count()

    @staticmethod
    def repeat(body: Node, minimum: int, maximum: int | None, times: int) -> Node:
        size = max(1, _saturate(times * body.size))
        return Node("repeat", size, body=body, minimum=minimum, maximum=maximum)

    def count(self) -> int:
        point = self.take()
        if not _is_digit(point):
            raise _fail()
        if point == _cp("0"):
            if _is_digit(self.peek()):
                raise _fail()
            return 0
        value = point - 0x30
        while _is_digit(self.peek()):
            value = value * 10 + (self.take() - 0x30)
            if value > MAX_COUNT:
                raise _fail()
        return value


def parse_pattern(points: list[int]) -> Node | None:
    """The tree for a pattern's code points, or None when it is not a valid pattern."""
    if len(points) > MAX_LENGTH:
        return None
    try:
        node = _Parser(points).parse()
    except _Invalid:
        return None
    return None if node.size > MAX_SIZE else node


# Program instructions: (op, a, b). CHAR a=CharSet; SPLIT a=x b=y; START, END, MATCH.
_CHAR, _SPLIT, _START_OP, _END_OP, _MATCH = range(5)

Program = list[list]  # [op, a, b], mutable so a split can be patched


def _emit_alt(sides: tuple[Node, ...], program: Program) -> None:
    jumps: list[list] = []
    last = len(sides) - 1
    for index, side in enumerate(sides):
        choice = [_SPLIT, len(program) + 1, len(program) + 1] if index < last else None
        if choice is not None:
            program.append(choice)
        _emit(side, program)
        if choice is None:
            continue
        jump = [_SPLIT, 0, 0]
        program.append(jump)
        jumps.append(jump)
        choice[2] = len(program)
    for jump in jumps:
        jump[1] = jump[2] = len(program)


def _emit_repeat(body: Node, minimum: int, maximum: int | None, program: Program) -> None:
    # A body of size 0 is empty groups only and matches just the empty string, so repeating it
    # emits nothing. Its copies would otherwise cost instructions that the size does not count.
    if body.size == 0:
        return
    for _ in range(minimum):
        _emit(body, program)
    if maximum is None:
        top = len(program)
        loop = [_SPLIT, top + 1, top + 1]
        program.append(loop)
        _emit(body, program)
        program.append([_SPLIT, top, top])
        loop[2] = len(program)
        return
    optional: list[list] = []
    for _ in range(minimum, maximum):
        choice = [_SPLIT, len(program) + 1, len(program) + 1]
        program.append(choice)
        optional.append(choice)
        _emit(body, program)
    for choice in optional:
        choice[2] = len(program)


def _emit(node: Node, program: Program) -> None:
    if node.kind == "set":
        program.append([_CHAR, node.charset, 0])
    elif node.kind == "start":
        program.append([_START_OP, 0, 0])
    elif node.kind == "end":
        program.append([_END_OP, 0, 0])
    elif node.kind == "concat":
        for part in node.parts:
            _emit(part, program)
    elif node.kind == "alt":
        _emit_alt(node.parts, program)
    else:
        assert node.body is not None
        _emit_repeat(node.body, node.minimum, node.maximum, program)


def compile_pattern(pattern: str) -> Program | None:
    """The compiled pattern, or None when ``pattern`` is not a valid pattern."""
    node = parse_pattern(code_points(pattern))
    if node is None:
        return None
    program: Program = []
    _emit(node, program)
    program.append([_MATCH, 0, 0])
    return program


def _in_item(item: Item, point: int) -> bool:
    ranges = item.ranges
    hit = False
    for index in range(0, len(ranges) - 1, 2):
        if ranges[index] <= point <= ranges[index + 1]:
            hit = True
            break
    return hit != item.negate


def _in_set(charset: CharSet, point: int) -> bool:
    return any(_in_item(item, point) for item in charset.items) != charset.negate


def pattern_matches(program: Program, text: str) -> bool:
    """Does the pattern match somewhere in ``text``? Each instruction is visited once per position."""
    points = code_points(text)
    length = len(points)
    seen = [-1] * len(program)
    stack: list[int] = []

    def add(found: list[int], pc: int, pos: int) -> bool:
        """Adds ``pc`` and all it reaches without reading; True when that reaches a match."""
        stack.append(pc)
        while stack:
            at = stack.pop()
            if seen[at] == pos:
                continue
            seen[at] = pos
            inst = program[at]
            op = inst[0]
            if op == _MATCH:
                stack.clear()
                return True
            if op == _CHAR:
                found.append(at)
            elif op == _SPLIT:
                stack.append(inst[2])
                stack.append(inst[1])
            elif (pos == 0) if op == _START_OP else (pos == length):
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
        point = points[pos]
        following.clear()
        for pc in current:
            if _in_set(program[pc][1], point) and add(following, pc + 1, pos + 1):
                return True
        current, following = following, current
        pos += 1
