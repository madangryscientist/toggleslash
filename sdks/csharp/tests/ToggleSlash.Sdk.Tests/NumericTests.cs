using System.Numerics;
using ToggleSlash.Sdk.Evaluation;
using ToggleSlash.Sdk.Tests.Support;

namespace ToggleSlash.Sdk.Tests;

public class NumericTests
{
    // A flag that serves variation 1 for a context whose attribute n is the number 5 (an "in" clause on [5]).
    private const string Flag = """{"key":"f","variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[{"clauses":[{"attribute":"n","op":"in","values":[5]}],"variation":1}],"fallthrough":{"variation":0},"salt":"s","version":1}""";

    public static TheoryData<object> EveryNumericType() =>
    [
        (sbyte)5, (byte)5, (short)5, (ushort)5, 5, 5u, 5L, 5UL, 5f, 5d, 5m, (nint)5, (nuint)5, (Half)5, (Int128)5, (UInt128)5, new BigInteger(5),
    ];

    [Theory]
    [MemberData(nameof(EveryNumericType))]
    public void ANumericAttributeOfAnyCSharpNumericTypeMatchesAsTheJsonNumber(object number)
    {
        var ruleset = Ruleset.Load(Harness.Json(Rulesets.Ruleset(Flag)));
        var context = new Dictionary<string, object?> { ["key"] = "u", ["attributes"] = new Dictionary<string, object?> { ["n"] = number } };
        var result = Evaluator.Evaluate(ruleset, "f", ContextConverter.Convert(context));
        Assert.Equal(1, result.VariationIndex);
        Assert.Equal(ReasonKind.RULE_MATCH, result.Reason.Kind);
    }

    [Fact]
    public void ANumericAttributeThatIsNotFiveDoesNotMatch()
    {
        var ruleset = Ruleset.Load(Harness.Json(Rulesets.Ruleset(Flag)));
        var context = new Dictionary<string, object?> { ["key"] = "u", ["attributes"] = new Dictionary<string, object?> { ["n"] = 6 } };
        Assert.Equal(0, Evaluator.Evaluate(ruleset, "f", ContextConverter.Convert(context)).VariationIndex);
    }

    [Fact]
    public void ADeeplyNestedContextIsRefusedAsInvalidAtFiveHundredAndTwelveLevels()
    {
        object nest = 1;
        for (var i = 0; i < 600; i++)
        {
            nest = new List<object?> { nest };
        }

        var ruleset = Ruleset.Load(Harness.Json(Rulesets.Ruleset(Flag)));
        var context = new Dictionary<string, object?> { ["key"] = "u", ["attributes"] = new Dictionary<string, object?> { ["n"] = nest } };
        var result = Evaluator.Evaluate(ruleset, "f", ContextConverter.Convert(context));
        Assert.Equal(ErrorCode.INVALID_CONTEXT, result.Reason.ErrorCode);
    }
}
