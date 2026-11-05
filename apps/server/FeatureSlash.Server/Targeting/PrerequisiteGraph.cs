using System.Text.Json;

namespace FeatureSlash.Server.Targeting;

/// <summary>One environment's prerequisite edges: flag key to the flag keys it requires.</summary>
internal static class PrerequisiteGraph
{
    /// <summary>The flag keys a stored <c>prerequisites</c> array names.</summary>
    public static List<string> Edges(string prerequisitesJson)
    {
        using var document = JsonDocument.Parse(prerequisitesJson);
        return document.RootElement.EnumerateArray()
            .Select(p => p.GetProperty("flagKey").GetString()!)
            .ToList();
    }

    /// <summary>
    /// A chain of prerequisites that leads from <paramref name="flag"/> back to itself once its edges are
    /// <paramref name="proposed"/>, as keys with the flag first and last (<c>a, b, a</c>), or null when
    /// there is none. A flag that is its own prerequisite is the shortest one.
    /// </summary>
    public static List<string>? CycleThrough(string flag, IReadOnlyList<string> proposed, IReadOnlyDictionary<string, List<string>> stored)
    {
        var parent = new Dictionary<string, string>(StringComparer.Ordinal);
        var queue = new Queue<string>();
        foreach (var next in proposed)
        {
            if (next == flag)
            {
                return [flag, flag];
            }
            if (parent.TryAdd(next, flag))
            {
                queue.Enqueue(next);
            }
        }
        while (queue.TryDequeue(out var current))
        {
            if (!stored.TryGetValue(current, out var edges))
            {
                continue;
            }
            foreach (var next in edges)
            {
                if (next == flag)
                {
                    var chain = new List<string> { flag, current };
                    for (var step = current; parent[step] != flag; step = parent[step])
                    {
                        chain.Insert(1, parent[step]);
                    }
                    chain.Add(flag);
                    return chain;
                }
                if (parent.TryAdd(next, current))
                {
                    queue.Enqueue(next);
                }
            }
        }
        return null;
    }
}
