using System.Text.RegularExpressions;

namespace ToggleSlash.Sdk.Tests;

/// <summary>Searches over the SDK's own source for the lines the checklist asks to be proven by search.</summary>
public partial class SourceTests
{
    private static readonly string Root = FindRoot();

    private static string FindRoot()
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir is not null && !File.Exists(Path.Combine(dir.FullName, "mise.toml")))
        {
            dir = dir.Parent;
        }

        return dir?.FullName ?? throw new InvalidOperationException("sdks/csharp not found");
    }

    /// <summary>The file's text without comment lines, so a doc comment may name what the code must not use.</summary>
    private static string Code(string path) =>
        string.Join('\n', File.ReadAllLines(path).Where(l => !l.TrimStart().StartsWith("//", StringComparison.Ordinal)));

    private static IEnumerable<string> Sources(string folder) =>
        Directory.EnumerateFiles(Path.Combine(Root, folder), "*.cs", SearchOption.AllDirectories)
            .Where(p => !p.Contains($"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}", StringComparison.Ordinal));

    [Fact]
    public void TheEvaluatorUsesNoPlatformRegexAndNoSystemTextJson()
    {
        var files = Sources("src/ToggleSlash.Sdk/Evaluation").ToArray();
        Assert.NotEmpty(files);
        foreach (var file in files)
        {
            var text = Code(file);
            Assert.DoesNotContain("System.Text.RegularExpressions", text, StringComparison.Ordinal);
            Assert.DoesNotContain("System.Text.Json", text, StringComparison.Ordinal);
            Assert.False(Regex.IsMatch(text, @"\bRegex\b"), file + " names Regex");
            Assert.DoesNotContain("JsonSerializer", text, StringComparison.Ordinal);
            Assert.DoesNotContain("JsonDocument", text, StringComparison.Ordinal);
        }
    }

    [Fact]
    public void NoSourceFileOfTheLibraryUsesSystemTextJsonOrRegex()
    {
        foreach (var file in Sources("src"))
        {
            var text = Code(file);
            Assert.DoesNotContain("System.Text.Json", text, StringComparison.Ordinal);
            Assert.DoesNotContain("System.Text.RegularExpressions", text, StringComparison.Ordinal);
        }
    }

    [Fact]
    public void TheLibraryAddsOnlyTheFourAllowedRequestHeaders()
    {
        var named = new SortedSet<string>(StringComparer.Ordinal);
        foreach (var file in Sources("src"))
        {
            var text = Code(file);
            foreach (Match m in HeaderAdd().Matches(text))
            {
                named.Add(m.Groups[1].Value);
            }

            foreach (Match m in HeadersProperty().Matches(text))
            {
                named.Add(m.Groups[1].Value);
            }

            Assert.DoesNotContain("DefaultRequestHeaders", text, StringComparison.Ordinal);
            Assert.DoesNotContain("UserAgent", text, StringComparison.Ordinal);
            Assert.DoesNotContain("AcceptEncoding", text, StringComparison.Ordinal);
        }

        Assert.Equal(["Accept", "Authorization", "ContentType", "If-None-Match"], named.ToArray());
    }

    [GeneratedRegex(@"Headers\.(?:TryAddWithoutValidation|Add)\(\s*""([^""]+)""")]
    private static partial Regex HeaderAdd();

    [GeneratedRegex(@"Headers\.(ContentType)\b")]
    private static partial Regex HeadersProperty();

    [Fact]
    public void TheLibraryLogsNothingAndPutsNoContextInAnErrorOrAMessage()
    {
        foreach (var file in Sources("src"))
        {
            var text = Code(file);
            foreach (var banned in new[] { "Console.", "Debug.", "Trace.", "ILogger", "Microsoft.Extensions.Logging", "EventSource", "Activity", "Process.Start" })
            {
                Assert.DoesNotContain(banned, text, StringComparison.Ordinal);
            }
        }

        // Every exception the SDK builds itself is one of two fixed shapes; neither mentions a context, a key or a flag.
        var thrown = new List<string>();
        foreach (var file in Sources("src"))
        {
            thrown.AddRange(NewException().Matches(Code(file)).Select(m => m.Value));
        }

        Assert.NotEmpty(thrown);
        foreach (var line in thrown)
        {
            Assert.DoesNotContain("context", line, StringComparison.OrdinalIgnoreCase);
            Assert.DoesNotContain("flagKey", line, StringComparison.Ordinal);
            Assert.DoesNotContain("sdkKey", line, StringComparison.OrdinalIgnoreCase);
        }
    }

    [GeneratedRegex(@"new ToggleSlashException\([^;]*;")]
    private static partial Regex NewException();

    [Fact]
    public void EveryDisabledLintRuleHasAReasonCommentAboveIt()
    {
        var lines = File.ReadAllLines(Path.Combine(Root, ".editorconfig"));
        var disabled = 0;
        for (var i = 0; i < lines.Length; i++)
        {
            if (!lines[i].StartsWith("dotnet_diagnostic.", StringComparison.Ordinal) || !lines[i].EndsWith("= none", StringComparison.Ordinal))
            {
                continue;
            }

            disabled++;
            var j = i - 1;
            while (j >= 0 && lines[j].StartsWith("dotnet_diagnostic.", StringComparison.Ordinal))
            {
                j--;
            }

            Assert.True(j >= 0 && lines[j].StartsWith('#') && lines[j].Length > 30, $"line {i + 1} ({lines[i]}) has no reason comment");
        }

        Assert.True(disabled > 0);
    }

    [Fact]
    public void NoSourceFileHoldsAnEmOrEnDash()
    {
        foreach (var path in Directory.EnumerateFiles(Root, "*", SearchOption.AllDirectories)
                     .Where(p => !p.Contains($"{Path.DirectorySeparatorChar}artifacts{Path.DirectorySeparatorChar}", StringComparison.Ordinal)
                                 && !p.Contains($"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}", StringComparison.Ordinal)
                                 && (p.EndsWith(".cs", StringComparison.Ordinal) || p.EndsWith(".md", StringComparison.Ordinal))))
        {
            var text = File.ReadAllText(path);
            Assert.False(text.Contains((char)0x2014) || text.Contains((char)0x2013), path + " holds a dash");
        }
    }
}
