"""The ``matches`` dialect of evaluation.md, "Patterns": a parser to a small program and a Pike VM.

No regular expression of the platform is used. Strings are Python ``str``, a sequence of code
points, which is the unit the spec wants (a lone surrogate is one code point).
"""

from __future__ import annotations

from functools import lru_cache

MAX_LENGTH = 1024
MAX_SIZE = 10000
MAX_COUNT = 100
MAX_CODE_POINT = 0x10FFFF

_META = frozenset("\\.[]()|*+?{}^$")
_SIMPLE_ESCAPES = {"n": 10, "t": 9, "r": 13, "f": 12, "v": 11}
_DIGITS = frozenset("0123456789")

_Ranges = tuple[tuple[int, int], ...]

_DIGIT_RANGES: _Ranges = ((48, 57),)
_WORD_RANGES: _Ranges = ((48, 57), (65, 90), (95, 95), (97, 122))
_SPACE_RANGES: _Ranges = ((9, 13), (32, 32))


def _complement(ranges: _Ranges) -> _Ranges:
    out = []
    low = 0
    for lo, hi in sorted(ranges):
        if lo > low:
            out.append((low, lo - 1))
        low = hi + 1
    if low <= MAX_CODE_POINT:
        out.append((low, MAX_CODE_POINT))
    return tuple(out)


_CLASS_ESCAPES: dict[str, _Ranges] = {
    "d": _DIGIT_RANGES,
    "w": _WORD_RANGES,
    "s": _SPACE_RANGES,
    "D": _complement(_DIGIT_RANGES),
    "W": _complement(_WORD_RANGES),
    "S": _complement(_SPACE_RANGES),
}


class _Invalid(Exception):
    """The pattern is not valid."""


# AST nodes, as tuples: ("lit", cp) ("any",) ("class", negated, ranges) ("bol",) ("eol",)
# ("cat", [nodes]) ("alt", [nodes]) ("rep", node, low, high-or-None, simple)


class _Parser:
    def __init__(self, text: str) -> None:
        self.text = text
        self.i = 0
        self.n = len(text)

    def parse(self) -> tuple:
        node = self.alternation()
        if self.i < self.n:
            raise _Invalid  # an unmatched ")"
        return node

    def alternation(self) -> tuple:
        sides = [self.concatenation()]
        while self.i < self.n and self.text[self.i] == "|":
            self.i += 1
            sides.append(self.concatenation())
        return sides[0] if len(sides) == 1 else ("alt", sides)

    def concatenation(self) -> tuple:
        items: list[tuple] = []
        while self.i < self.n and self.text[self.i] not in "|)":
            node, quantifiable = self.atom()
            if self.i < self.n and self.text[self.i] in "*+?{":
                if not quantifiable:
                    raise _Invalid
                simple = self.text[self.i] in "*+?"
                low, high = self.quantifier()
                node = ("rep", node, low, high, simple)
                if self.i < self.n and self.text[self.i] in "*+?{":
                    raise _Invalid  # a quantifier after a quantifier
            items.append(node)
        return ("cat", items)

    def atom(self) -> tuple[tuple, bool]:
        ch = self.text[self.i]
        if ch in "*+?":
            raise _Invalid
        if ch in "{}]":
            raise _Invalid
        if ch == "(":
            self.i += 1
            if self.i < self.n and self.text[self.i] == "?":
                raise _Invalid
            inner = self.alternation()
            if self.i >= self.n or self.text[self.i] != ")":
                raise _Invalid
            self.i += 1
            return inner, True
        if ch == "[":
            return self.char_class(), True
        if ch == ".":
            self.i += 1
            return ("any",), True
        if ch == "^":
            self.i += 1
            return ("bol",), False
        if ch == "$":
            self.i += 1
            return ("eol",), False
        if ch == "\\":
            esc = self.escape()
            if isinstance(esc, int):
                return ("lit", esc), True
            return ("class", False, esc), True
        self.i += 1
        return ("lit", ord(ch)), True

    def escape(self) -> int | _Ranges:
        """Read ``\\x`` at the cursor: a code point, or the ranges of ``\\d \\w \\s \\D \\W \\S``."""
        if self.i + 1 >= self.n:
            raise _Invalid
        ch = self.text[self.i + 1]
        self.i += 2
        if ch in _META or ch == "-":
            return ord(ch)
        if ch in _SIMPLE_ESCAPES:
            return _SIMPLE_ESCAPES[ch]
        if ch in _CLASS_ESCAPES:
            return _CLASS_ESCAPES[ch]
        raise _Invalid

    def number(self) -> int:
        start = self.i
        while self.i < self.n and self.text[self.i] in _DIGITS:
            self.i += 1
        digits = self.text[start : self.i]
        if digits == "" or (len(digits) > 1 and digits[0] == "0") or len(digits) > 3:
            raise _Invalid
        value = int(digits)
        if value > MAX_COUNT:
            raise _Invalid
        return value

    def quantifier(self) -> tuple[int, int | None]:
        ch = self.text[self.i]
        if ch == "*":
            self.i += 1
            return 0, None
        if ch == "+":
            self.i += 1
            return 1, None
        if ch == "?":
            self.i += 1
            return 0, 1
        self.i += 1  # "{"
        low = self.number()
        high: int | None = low
        if self.i < self.n and self.text[self.i] == ",":
            self.i += 1
            if self.i < self.n and self.text[self.i] == "}":
                high = None
            else:
                high = self.number()
                if high < low:
                    raise _Invalid
        if self.i >= self.n or self.text[self.i] != "}":
            raise _Invalid
        self.i += 1
        return low, high

    def class_atom(self, first: bool) -> int | _Ranges:
        ch = self.text[self.i]
        if ch == "[":
            raise _Invalid
        if ch == "\\":
            return self.escape()
        if ch == "-":
            directly_before_close = self.i + 1 < self.n and self.text[self.i + 1] == "]"
            if not (first or directly_before_close):
                raise _Invalid
        self.i += 1
        return ord(ch)

    def char_class(self) -> tuple:
        self.i += 1  # "["
        negated = False
        if self.i < self.n and self.text[self.i] == "^":
            negated = True
            self.i += 1
        ranges: list[tuple[int, int]] = []
        first = True
        while True:
            if self.i >= self.n:
                raise _Invalid
            if self.text[self.i] == "]":
                if first:
                    raise _Invalid
                self.i += 1
                break
            a = self.class_atom(first)
            first = False
            if self.i + 1 < self.n and self.text[self.i] == "-" and self.text[self.i + 1] != "]":
                self.i += 1
                b = self.class_atom(False)
                if not isinstance(a, int) or not isinstance(b, int) or a > b:
                    raise _Invalid
                ranges.append((a, b))
            elif isinstance(a, int):
                ranges.append((a, a))
            else:
                ranges.extend(a)
        return ("class", negated, tuple(ranges))


def _size(node: tuple) -> int:
    kind = node[0]
    if kind == "cat":
        return sum(_size(part) for part in node[1])
    if kind == "alt":
        return sum(_size(part) for part in node[1]) + len(node[1]) - 1
    if kind == "rep":
        body = _size(node[1])
        low, high, simple = node[2], node[3], node[4]
        if simple:
            total = body + 1  # x*, x+ and x?
        elif high is None:
            total = (low + 1) * body  # x{n,}
        else:
            total = high * body  # x{n} and x{n,m}
        return max(1, total)
    return 1


# Instruction opcodes.
_CHAR, _ANY, _CLASS, _SPLIT, _JMP, _BOL, _EOL, _MATCH = range(8)


def _emit(node: tuple, prog: list[tuple]) -> None:
    kind = node[0]
    if kind == "lit":
        prog.append((_CHAR, node[1]))
    elif kind == "any":
        prog.append((_ANY,))
    elif kind == "class":
        prog.append((_CLASS, node[1], node[2]))
    elif kind == "bol":
        prog.append((_BOL,))
    elif kind == "eol":
        prog.append((_EOL,))
    elif kind == "cat":
        for part in node[1]:
            _emit(part, prog)
    elif kind == "alt":
        jumps = []
        sides = node[1]
        for index, side in enumerate(sides):
            if index < len(sides) - 1:
                split = len(prog)
                prog.append((_SPLIT, split + 1, -1))
                _emit(side, prog)
                jumps.append(len(prog))
                prog.append((_JMP, -1))
                prog[split] = (_SPLIT, split + 1, len(prog))
            else:
                _emit(side, prog)
        end = len(prog)
        for j in jumps:
            prog[j] = (_JMP, end)
    else:  # rep
        body, low, high = node[1], node[2], node[3]
        if _size(body) == 0:
            return  # an empty body matches the empty string and repeating it changes nothing
        for _ in range(low):
            _emit(body, prog)
        if high is None:
            split = len(prog)
            prog.append((_SPLIT, split + 1, -1))
            _emit(body, prog)
            prog.append((_JMP, split))
            prog[split] = (_SPLIT, split + 1, len(prog))
        else:
            splits = []
            for _ in range(high - low):
                splits.append(len(prog))
                prog.append((_SPLIT, len(prog) + 1, -1))
                _emit(body, prog)
            end = len(prog)
            for s in splits:
                prog[s] = (_SPLIT, s + 1, end)


class Pattern:
    """A compiled pattern."""

    __slots__ = ("prog",)

    def __init__(self, prog: tuple[tuple, ...]) -> None:
        self.prog = prog


@lru_cache(maxsize=512)
def compile_pattern(text: str) -> Pattern | None:
    """Compile ``text``, or ``None`` when it is not a valid pattern (including the limits)."""
    if len(text) > MAX_LENGTH:
        return None
    try:
        tree = _Parser(text).parse()
    except _Invalid:
        return None
    if _size(tree) > MAX_SIZE:
        return None
    prog: list[tuple] = []
    _emit(tree, prog)
    prog.append((_MATCH,))
    return Pattern(tuple(prog))


def _class_has(ranges: _Ranges, cp: int) -> bool:
    return any(lo <= cp <= hi for lo, hi in ranges)


def search(pattern: Pattern, text: str) -> bool:
    """True when the pattern matches somewhere in ``text`` (a Pike VM: each instruction at most once
    per input position, so the time is bounded by size x input whatever the pattern)."""
    prog = pattern.prog
    length = len(text)
    mark = [-1] * len(prog)
    current: list[int] = []

    def add(threads: list[int], start: int, pos: int) -> bool:
        stack = [start]
        while stack:
            pc = stack.pop()
            if mark[pc] == pos:
                continue
            mark[pc] = pos
            ins = prog[pc]
            op = ins[0]
            if op == _JMP:
                stack.append(ins[1])
            elif op == _SPLIT:
                stack.append(ins[2])
                stack.append(ins[1])
            elif op == _BOL:
                if pos == 0:
                    stack.append(pc + 1)
            elif op == _EOL:
                if pos == length:
                    stack.append(pc + 1)
            elif op == _MATCH:
                return True
            else:
                threads.append(pc)
        return False

    for pos in range(length + 1):
        if add(current, 0, pos):
            return True
        if pos == length:
            break
        cp = ord(text[pos])
        following: list[int] = []
        for pc in current:
            ins = prog[pc]
            op = ins[0]
            if op == _CHAR:
                ok = ins[1] == cp
            elif op == _ANY:
                ok = cp != 10
            else:
                ok = _class_has(ins[2], cp) != ins[1]
            if ok and add(following, pc + 1, pos + 1):
                return True
        current = following
    return False


def code_points(text: str) -> str:
    """``text`` with every valid surrogate pair joined into one code point (a lone surrogate stays).

    Python strings from JSON already join pairs; this covers a string an application built from halves.
    """
    if text.isascii() or not any("\ud800" <= ch <= "\udfff" for ch in text):
        return text
    out = []
    i = 0
    n = len(text)
    while i < n:
        ch = text[i]
        if "\ud800" <= ch <= "\udbff" and i + 1 < n and "\udc00" <= text[i + 1] <= "\udfff":
            out.append(chr(0x10000 + ((ord(ch) - 0xD800) << 10) + (ord(text[i + 1]) - 0xDC00)))
            i += 2
        else:
            out.append(ch)
            i += 1
    return "".join(out)
