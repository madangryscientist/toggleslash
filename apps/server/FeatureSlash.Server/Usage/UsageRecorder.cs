using System.Globalization;
using System.Text.Json;
using FeatureSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Usage;

/// <summary>One flag served one variation <see cref="Count"/> times on <see cref="Day"/> (UTC). Counts only: no context, no key.</summary>
internal sealed record UsageEntry(string FlagKey, int Variation, long Count, DateOnly Day);

/// <param name="Accepted">Entries written to a known flag.</param>
/// <param name="UnknownFlags">Entries ignored because the environment's project has no flag with that key.</param>
/// <param name="OutOfRange">The first entry whose variation is not an index into its flag's variations; when set, nothing was written.</param>
internal sealed record UsageResult(int Accepted, int UnknownFlags, UsageEntry? OutOfRange = null);

/// <summary>
/// Adds to <c>flag_usage_daily</c>. The environment is always the caller's own (from its SDK key), never
/// something the request names, so a batch cannot write to another environment, and a flag is looked up in that
/// environment's project only.
/// </summary>
internal sealed class UsageRecorder(FeatureSlashDbContext database, ILogger<UsageRecorder> logger)
{
    public async Task<UsageResult> Record(long environmentId, IReadOnlyCollection<UsageEntry> entries, CancellationToken cancellation)
    {
        if (entries.Count == 0)
        {
            return new UsageResult(0, 0);
        }

        var projectId = await database.Environments.AsNoTracking()
            .Where(e => e.Id == environmentId)
            .Select(e => e.ProjectId)
            .SingleAsync(cancellation);
        var keys = entries.Select(e => e.FlagKey).Distinct().ToList();
        var flags = await database.Flags.AsNoTracking()
            .Where(f => f.ProjectId == projectId && keys.Contains(f.Key))
            .Select(f => new { f.Id, f.Key, f.Variations })
            .ToDictionaryAsync(f => f.Key, cancellation);

        var known = entries.Where(e => flags.ContainsKey(e.FlagKey)).ToList();
        foreach (var entry in known)
        {
            if (entry.Variation >= VariationCount(flags[entry.FlagKey].Variations))
            {
                return new UsageResult(0, 0, entry);
            }
        }

        // Entries for the same flag, variation and day are one row: sum them first.
        var rows = known
            .GroupBy(e => (e.FlagKey, e.Variation, e.Day))
            .Select(g => (flags[g.Key.FlagKey].Id, g.Key.Variation, g.Key.Day, Count: g.Sum(e => e.Count)))
            .ToList();

        await using var transaction = await database.Database.BeginTransactionAsync(cancellation);
        foreach (var (flagId, variation, day, count) in rows)
        {
            // Additive: the database adds, so two batches at once cannot lose a count the way read-then-write would.
            await database.Database.ExecuteSqlInterpolatedAsync($"""
                INSERT INTO flag_usage_daily (environment_id, flag_id, variation, day, "count")
                VALUES ({environmentId}, {flagId}, {variation}, {day.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture)}, {count})
                ON CONFLICT (environment_id, flag_id, variation, day) DO UPDATE SET "count" = "count" + excluded."count"
                """, cancellation);
        }
        await transaction.CommitAsync(cancellation);
        return new UsageResult(known.Count, entries.Count - known.Count);
    }

    /// <summary>
    /// What <c>evaluate</c> counts about itself. Counting is a side effect of serving a flag, so a failure to
    /// count is logged and never turns a good answer into an error.
    /// </summary>
    public async Task RecordEvaluations(long environmentId, IReadOnlyCollection<UsageEntry> entries, CancellationToken cancellation)
    {
        try
        {
            await Record(environmentId, entries, cancellation);
        }
        catch (Exception exception) when (exception is DbUpdateException or Microsoft.Data.Sqlite.SqliteException)
        {
            logger.LogWarning(exception, "Could not record the usage of an evaluation; the evaluation itself was served.");
        }
    }

    private static int VariationCount(string variations)
    {
        using var document = JsonDocument.Parse(variations);
        return document.RootElement.GetArrayLength();
    }
}
