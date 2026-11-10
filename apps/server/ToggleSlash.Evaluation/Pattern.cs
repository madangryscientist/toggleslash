namespace ToggleSlash.Evaluation;

/// <summary>
/// A pattern for the <c>matches</c> op, to the "Patterns" section of docs/evaluation.md. A hand-written parser
/// to a small program and a Pike VM over code points: no backtracking and no
/// <c>System.Text.RegularExpressions</c>, so a match costs O(program x input) whatever the pattern, and it
/// reads the same way as the TypeScript one. Strings are read as code points by hand, not as <c>Rune</c>s,
/// because a lone surrogate is a code point of its own here and <c>Rune</c> turns it into U+FFFD.
/// </summary>
internal sealed class Pattern
{
    private const int MaxLength = 1024;
    private const int MaxSize = 10000;
    private const int MaxCount = 100;
    private const int Newline = 0x0A;

    private readonly Inst[] _program;

    private Pattern(Inst[] program) => _program = program;

    /// <summary>Code point ranges as pairs lo, hi, inclusive, or their complement when <see cref="Negate"/> is set.</summary>
    private sealed record Item(int[] Ranges, bool Negate)
    {
        public bool Contains(int c)
        {
            var hit = false;
            for (var i = 0; i < Ranges.Length && !hit; i += 2) hit = Ranges[i] <= c && c <= Ranges[i + 1];
            return hit != Negate;
        }
    }

    /// <summary>A class: any of its items, or none of them when <see cref="Negate"/> is set.</summary>
    private sealed record CharSet(Item[] Items, bool Negate)
    {
        public bool Contains(int c) => Items.Any(item => item.Contains(c)) != Negate;
    }

    private abstract record Node(int Size);

    private sealed record SetNode(CharSet Set) : Node(1);

    private sealed record StartNode() : Node(1);

    private sealed record EndNode() : Node(1);

    private sealed record ConcatNode(List<Node> Parts, int Size) : Node(Size);

    private sealed record AltNode(List<Node> Sides, int Size) : Node(Size);

    private sealed record RepeatNode(Node Body, int Min, int? Max, int Size) : Node(Size);

    private enum OpCode
    {
        Char,
        Split,
        Jmp,
        Start,
        End,
        Match,
    }

    private sealed class Inst(OpCode op, CharSet? set = null)
    {
        public OpCode Op { get; } = op;
        public CharSet? Set { get; } = set;
        public int X { get; set; }
        public int Y { get; set; }
    }

    private static readonly Item Digit = new([0x30, 0x39], false);
    private static readonly Item Word = new([0x30, 0x39, 0x41, 0x5A, 0x5F, 0x5F, 0x61, 0x7A], false);
    // U+0009 to U+000D are \t \n \v \f \r.
    private static readonly Item Space = new([0x09, 0x0D, 0x20, 0x20], false);
    private static readonly CharSet Any = new([new Item([Newline, Newline], true)], false);

    /// <summary><c>\</c> before one of these is that character.</summary>
    private const string Escapable = "\\.[]()|*+?{}^$-";

    /// <summary>The code points of a UTF-16 string: a valid surrogate pair is one, a lone surrogate is itself.</summary>
    private static List<int> CodePoints(string text)
    {
        var points = new List<int>(text.Length);
        for (var i = 0; i < text.Length; i++)
        {
            if (i + 1 < text.Length && char.IsSurrogatePair(text[i], text[i + 1]))
            {
                points.Add(char.ConvertToUtf32(text[i], text[i + 1]));
                i++;
            }
            else
            {
                points.Add(text[i]);
            }
        }
        return points;
    }

    private static int Saturate(int size) => Math.Min(size, MaxSize + 1);

    private static bool IsDigit(int c) => c >= '0' && c <= '9';

    private static Item Single(int c) => new([c, c], false);

    /// <summary>The compiled pattern, or null when <paramref name="pattern"/> is not a valid pattern.</summary>
    public static Pattern? Compile(string pattern)
    {
        var cps = CodePoints(pattern);
        if (cps.Count > MaxLength) return null;
        var node = new Parser(cps).Parse();
        if (node is null || node.Size > MaxSize) return null;
        var program = new List<Inst>();
        Emit(node, program);
        program.Add(new Inst(OpCode.Match));
        return new Pattern([.. program]);
    }

    private sealed class Parser(List<int> cps)
    {
        private int _pos;

        /// <summary>Thrown on an invalid pattern and caught in <see cref="Parse"/>; it never leaves this class.</summary>
        private sealed class InvalidException : Exception;

        private static T Fail<T>() => throw new InvalidException();

        private int Peek(int offset = 0) => _pos + offset < cps.Count ? cps[_pos + offset] : -1;

        public Node? Parse()
        {
            try
            {
                var node = Alternation();
                return _pos < cps.Count ? null : node; // an unmatched ')'
            }
            catch (InvalidException)
            {
                return null;
            }
        }

        private Node Alternation()
        {
            var sides = new List<Node> { Concat() };
            while (Peek() == '|')
            {
                _pos++;
                sides.Add(Concat());
            }
            if (sides.Count == 1) return sides[0];
            return new AltNode(sides, Saturate(sides.Sum(side => side.Size) + sides.Count - 1));
        }

        private Node Concat()
        {
            var parts = new List<Node>();
            for (var c = Peek(); c != -1 && c != '|' && c != ')'; c = Peek())
            {
                var atom = Atom();
                var next = Peek();
                var quantified = atom is not StartNode and not EndNode && next is '*' or '+' or '?' or '{';
                parts.Add(quantified ? Quantify(atom) : atom);
            }
            return new ConcatNode(parts, Saturate(parts.Sum(part => part.Size)));
        }

        private Node Atom()
        {
            var c = Peek();
            _pos++;
            switch (c)
            {
                case '(':
                    var inner = Alternation();
                    if (Peek() != ')') return Fail<Node>();
                    _pos++;
                    return inner;
                case '[':
                    return new SetNode(CharClass());
                case '.':
                    return new SetNode(Any);
                case '^':
                    return new StartNode();
                case '$':
                    return new EndNode();
                case '\\':
                    var escaped = Escape();
                    return new SetNode(new CharSet([escaped.Item ?? Single(escaped.Point)], false));
                case '*' or '+' or '?' or '{' or '}' or ']':
                    return Fail<Node>();
                default:
                    return new SetNode(new CharSet([Single(c)], false));
            }
        }

        /// <summary>After a <c>\</c>: one code point, or one of the six shorthand classes (<c>Item</c> set).</summary>
        private (int Point, Item? Item) Escape()
        {
            var c = Peek();
            if (c == -1) return Fail<(int, Item?)>();
            _pos++;
            if (c < 0x80 && Escapable.Contains((char)c)) return (c, null);
            return c switch
            {
                'n' => (0x0A, null),
                't' => (0x09, null),
                'r' => (0x0D, null),
                'f' => (0x0C, null),
                'v' => (0x0B, null),
                'd' => (0, Digit),
                'w' => (0, Word),
                's' => (0, Space),
                'D' => (0, Digit with { Negate = true }),
                'W' => (0, Word with { Negate = true }),
                'S' => (0, Space with { Negate = true }),
                _ => Fail<(int, Item?)>(),
            };
        }

        /// <summary>After <c>[</c>: the members up to <c>]</c>.</summary>
        private CharSet CharClass()
        {
            var negate = Peek() == '^';
            if (negate) _pos++;
            var items = new List<Item>();
            for (var first = true; ; first = false)
            {
                var c = Peek();
                if (c == -1) return Fail<CharSet>();
                if (c == ']')
                {
                    if (first) return Fail<CharSet>();
                    _pos++;
                    return new CharSet([.. items], negate);
                }
                var from = ClassAtom(first);
                if (Peek() == '-' && Peek(1) != -1 && Peek(1) != ']')
                {
                    _pos++;
                    var to = ClassAtom(false);
                    if (from.Item is not null || to.Item is not null || from.Point > to.Point) return Fail<CharSet>();
                    items.Add(new Item([from.Point, to.Point], false));
                }
                else
                {
                    items.Add(from.Item ?? Single(from.Point));
                }
            }
        }

        private (int Point, Item? Item) ClassAtom(bool first)
        {
            var c = Peek();
            if (c == -1 || c == '[') return Fail<(int, Item?)>();
            _pos++;
            if (c == '\\') return Escape();
            // An unescaped '-' is a literal only as the first member or directly before ']'.
            if (c == '-' && !first && Peek() != ']') return Fail<(int, Item?)>();
            return (c, null);
        }

        private Node Quantify(Node body)
        {
            var c = Peek();
            _pos++;
            if (c != '{')
            {
                return new RepeatNode(body, c == '+' ? 1 : 0, c == '?' ? 1 : null, Saturate(body.Size + 1));
            }
            var min = Count();
            int? max = min;
            var times = min;
            if (Peek() == ',')
            {
                _pos++;
                if (Peek() == '}')
                {
                    max = null;
                    times = min + 1;
                }
                else
                {
                    max = Count();
                    times = max.Value;
                }
            }
            if (Peek() != '}' || (max is { } upper && min > upper)) return Fail<Node>();
            _pos++;
            return new RepeatNode(body, min, max, Math.Max(1, Saturate(times * body.Size)));
        }

        /// <summary>ASCII digits with no leading zero, at most 100.</summary>
        private int Count()
        {
            var c = Peek();
            if (!IsDigit(c)) return Fail<int>();
            _pos++;
            if (c == '0') return IsDigit(Peek()) ? Fail<int>() : 0;
            var value = c - '0';
            for (var d = Peek(); IsDigit(d); d = Peek())
            {
                value = value * 10 + (d - '0');
                if (value > MaxCount) return Fail<int>();
                _pos++;
            }
            return value;
        }
    }

    private static void Emit(Node node, List<Inst> program)
    {
        switch (node)
        {
            case SetNode set:
                program.Add(new Inst(OpCode.Char, set.Set));
                return;
            case StartNode:
                program.Add(new Inst(OpCode.Start));
                return;
            case EndNode:
                program.Add(new Inst(OpCode.End));
                return;
            case ConcatNode concat:
                foreach (var part in concat.Parts) Emit(part, program);
                return;
            case AltNode alt:
                var jumps = new List<Inst>();
                for (var i = 0; i < alt.Sides.Count; i++)
                {
                    if (i == alt.Sides.Count - 1)
                    {
                        Emit(alt.Sides[i], program);
                        break;
                    }
                    var split = new Inst(OpCode.Split) { X = program.Count + 1 };
                    program.Add(split);
                    Emit(alt.Sides[i], program);
                    var jump = new Inst(OpCode.Jmp);
                    jumps.Add(jump);
                    program.Add(jump);
                    split.Y = program.Count;
                }
                foreach (var jump in jumps) jump.X = program.Count;
                return;
            case RepeatNode repeat:
                // A body of size 0 is empty groups only and matches just the empty string, so repeating it
                // emits nothing. Its copies would otherwise cost instructions that the size does not count.
                if (repeat.Body.Size == 0) return;
                for (var i = 0; i < repeat.Min; i++) Emit(repeat.Body, program);
                if (repeat.Max is not { } max)
                {
                    var loop = program.Count;
                    var star = new Inst(OpCode.Split) { X = loop + 1 };
                    program.Add(star);
                    Emit(repeat.Body, program);
                    program.Add(new Inst(OpCode.Jmp) { X = loop });
                    star.Y = program.Count;
                    return;
                }
                var splits = new List<Inst>();
                for (var i = repeat.Min; i < max; i++)
                {
                    var split = new Inst(OpCode.Split) { X = program.Count + 1 };
                    splits.Add(split);
                    program.Add(split);
                    Emit(repeat.Body, program);
                }
                foreach (var split in splits) split.Y = program.Count;
                return;
        }
    }

    /// <summary>Does the pattern match somewhere in <paramref name="text"/>? Each instruction is visited once per position.</summary>
    public bool IsMatch(string text)
    {
        var input = CodePoints(text);
        var length = input.Count;
        var seen = new int[_program.Length];
        Array.Fill(seen, -1);
        var stack = new Stack<int>();
        var current = new List<int>();
        var next = new List<int>();

        // Adds pc and everything it reaches without reading to list; true when that reaches a match.
        bool Add(List<int> list, int pc, int pos)
        {
            stack.Clear();
            stack.Push(pc);
            while (stack.Count > 0)
            {
                var at = stack.Pop();
                if (seen[at] == pos) continue;
                seen[at] = pos;
                var inst = _program[at];
                switch (inst.Op)
                {
                    case OpCode.Char:
                        list.Add(at);
                        break;
                    case OpCode.Jmp:
                        stack.Push(inst.X);
                        break;
                    case OpCode.Split:
                        stack.Push(inst.Y);
                        stack.Push(inst.X);
                        break;
                    case OpCode.Start:
                        if (pos == 0) stack.Push(at + 1);
                        break;
                    case OpCode.End:
                        if (pos == length) stack.Push(at + 1);
                        break;
                    case OpCode.Match:
                        return true;
                }
            }
            return false;
        }

        for (var pos = 0; ; pos++)
        {
            if (Add(current, 0, pos)) return true;
            if (pos == length) return false;
            next.Clear();
            foreach (var pc in current)
            {
                if (_program[pc].Set!.Contains(input[pos]) && Add(next, pc + 1, pos + 1)) return true;
            }
            (current, next) = (next, current);
        }
    }
}
