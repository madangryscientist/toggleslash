using System.Data;
using System.Security.Cryptography;
using System.Text.Json;
using FeatureSlash.Evaluation;
using FeatureSlash.Server.Data;
using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Sdk;

/// <summary>A flag in the ruleset: <c>docs/evaluation.md</c>'s flag, plus the <c>version</c> it carries for the API.</summary>
public sealed record RulesetFlag(
    string Key,
    JsonElement Variations,
    bool On,
    int? OffVariation,
    JsonElement Targets,
    JsonElement Prerequisites,
    JsonElement Rules,
    JsonElement Fallthrough,
    string Salt,
    long Version);

public sealed record RulesetSegment(string Key, string Salt, string[] Included, string[] Excluded, JsonElement Rules);

/// <summary>One environment's non-archived flags and its segments, in the shape of <c>docs/evaluation.md</c>.</summary>
public sealed record RulesetResponse(RulesetFlag[] Flags, RulesetSegment[] Segments);

/// <summary>
/// An environment's ruleset as served. <see cref="Body"/> is exactly what the ruleset route sends and
/// <see cref="ETag"/> is the strong tag of those bytes, so the tag changes when, and only when, the body does.
/// </summary>
internal sealed class RulesetSnapshot(RulesetResponse ruleset, byte[] body)
{
    private static readonly JsonSerializerOptions WireOptions = new(JsonSerializerDefaults.Web);

    private string? hash;

    public byte[] Body { get; } = body;

    /// <summary>SHA-256 of <see cref="Body"/>, lowercase hex. Hashed on demand: <c>evaluate</c> never asks for it.</summary>
    public string Hash => hash ??= Convert.ToHexStringLower(SHA256.HashData(Body));

    public string ETag => $"\"{Hash}\"";

    /// <summary>Keys of the flags in the ruleset, in key order.</summary>
    public IReadOnlyList<string> FlagKeys { get; } = [.. ruleset.Flags.Select(flag => flag.Key)];

    /// <summary>The ruleset as <c>FeatureSlash.Evaluation</c> loads it, from the same bytes that were served.</summary>
    public Ruleset Load()
    {
        using var document = JsonDocument.Parse(Body);
        return Ruleset.Load(document.RootElement);
    }

    public static RulesetSnapshot Of(RulesetResponse ruleset) =>
        new(ruleset, JsonSerializer.SerializeToUtf8Bytes(ruleset, WireOptions));

    /// <summary>A body this class serialized, read back.</summary>
    public static RulesetResponse Parse(byte[] body) => JsonSerializer.Deserialize<RulesetResponse>(body, WireOptions)!;
}

/// <summary>Builds <see cref="RulesetSnapshot"/>s from the database. Nothing is cached: a write is visible to the next read.</summary>
internal sealed class RulesetReader(FeatureSlashDbContext database, ILogger<RulesetReader> logger)
{
    /// <summary>The environment is the one an SDK key belongs to, and no route deletes an environment, so it exists.</summary>
    public async Task<RulesetSnapshot> Read(long environmentId, CancellationToken cancellation)
    {
        // One read transaction (the database is in WAL mode), so flags and segments come from the same moment.
        // Deferred: Microsoft.Data.Sqlite's default would take the write lock (BEGIN IMMEDIATE) on every SDK read.
        var connection = (SqliteConnection)database.Database.GetDbConnection();
        await connection.OpenAsync(cancellation);
        await using var read = connection.BeginTransaction(IsolationLevel.Serializable, deferred: true);
        await using var transaction = await database.Database.UseTransactionAsync(read, cancellation);
        var environment = await database.Environments.AsNoTracking()
            .SingleAsync(e => e.Id == environmentId, cancellation);

        var flags = await (
            from flag in database.Flags.AsNoTracking()
            join config in database.FlagEnvironmentConfigs.AsNoTracking() on flag.Id equals config.FlagId
            where flag.ProjectId == environment.ProjectId && flag.ArchivedAt == null && config.EnvironmentId == environmentId
            orderby flag.Key
            select new { flag, config }).ToListAsync(cancellation);
        var segments = await database.Segments.AsNoTracking()
            .Where(s => s.EnvironmentId == environmentId)
            .OrderBy(s => s.Key)
            .ToListAsync(cancellation);

        return RulesetSnapshot.Of(new RulesetResponse(
            [.. flags.Select(f => new RulesetFlag(
                f.flag.Key,
                Json(f.flag.Variations),
                f.config.On,
                f.config.OffVariation,
                Json(f.config.Targets),
                Json(f.config.Prerequisites),
                Json(f.config.Rules),
                Json(f.config.Fallthrough),
                f.flag.Salt,
                f.config.Version))],
            [.. segments.Select(s => new RulesetSegment(
                s.Key,
                s.Salt,
                JsonSerializer.Deserialize<string[]>(s.Included)!,
                JsonSerializer.Deserialize<string[]>(s.Excluded)!,
                Json(s.Rules)))]));
    }

    /// <summary>
    /// Keeps a body the ruleset route is about to send, so its hash can be looked up later. A body already kept takes
    /// no write lock; two first reads at once are one row, with the first one's time. Keeping it is a side effect of
    /// serving, so a failure is logged and never costs an SDK its flags; the next 200 of that body tries again.
    /// </summary>
    public async Task RecordServed(long environmentId, RulesetSnapshot snapshot, CancellationToken cancellation)
    {
        var hash = snapshot.Hash;
        try
        {
            if (await database.RulesetSnapshots.AsNoTracking().AnyAsync(s => s.EnvironmentId == environmentId && s.Hash == hash, cancellation))
            {
                return;
            }
            await database.Database.ExecuteSqlInterpolatedAsync($"""
                INSERT INTO ruleset_snapshots (environment_id, hash, body, first_served_at)
                VALUES ({environmentId}, {hash}, {snapshot.Body}, {DateTime.UtcNow})
                ON CONFLICT (environment_id, hash) DO NOTHING
                """, cancellation);
        }
        catch (SqliteException exception)
        {
            logger.LogWarning(exception, "Could not keep a served ruleset; it was served, and its hash cannot be looked up yet.");
        }
    }

    private static JsonElement Json(string text)
    {
        using var document = JsonDocument.Parse(text);
        return document.RootElement.Clone();
    }
}
