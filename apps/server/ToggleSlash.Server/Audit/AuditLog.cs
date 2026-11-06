using System.Globalization;
using System.Text.Json;
using ToggleSlash.Server.Data;

namespace ToggleSlash.Server.Audit;

/// <summary>The kinds of entity an audit event describes.</summary>
internal static class AuditEntity
{
    public const string Project = "project";
    public const string Environment = "environment";
    public const string Flag = "flag";

    /// <summary>A flag's settings in one environment. Its id is <c>&lt;flag&gt;/&lt;env&gt;</c>.</summary>
    public const string FlagConfig = "flag_config";

    public const string Segment = "segment";
    public const string ApiKey = "api_key";
    public const string AdminToken = "admin_token";
}

internal static class AuditAction
{
    public const string Created = "created";
    public const string Updated = "updated";
    public const string Toggled = "toggled";
    public const string Archived = "archived";
    public const string Restored = "restored";
    public const string Revoked = "revoked";
    public const string Deleted = "deleted";
}

/// <summary>
/// Where an audit event belongs: the project, and the environment and flag when it concerns them. Keys,
/// not ids, because keys are fixed once created and an event outlives its entity.
/// </summary>
internal readonly record struct AuditScope(string Project, string? Environment = null, string? Flag = null);

/// <summary>
/// Every admin write records exactly one event, through <see cref="Record"/>, in the same
/// <c>SaveChanges</c> or explicit transaction as the write itself: both happen or neither does.
/// </summary>
internal static class AuditLog
{
    /// <summary>The actor of a write made with the root admin token.</summary>
    public const string Admin = "admin";

    /// <summary>The actor of a write made with a scoped token: the id says which one, and never anything secret.</summary>
    public static string ScopedToken(long id) => $"token:{id.ToString(CultureInfo.InvariantCulture)}";

    private static readonly JsonSerializerOptions SnapshotOptions = new(JsonSerializerDefaults.Web);

    /// <summary>
    /// Adds the event to the context; it is written by the caller's next <c>SaveChanges</c>. <c>before</c> and
    /// <c>after</c> are the API's own response shapes, which hold nothing private: no key hash, no plain key
    /// and no salt (the shapes do not carry them, and the audit tests check the stored rows for them).
    /// </summary>
    public static void Record(
        this ToggleSlashDbContext database,
        string entityType,
        string entityId,
        string action,
        AuditScope scope,
        object? before,
        object? after) =>
        database.AuditEvents.Add(new AuditEvent
        {
            OccurredAt = DateTime.UtcNow,
            Actor = database.AuditActor,
            EntityType = entityType,
            EntityId = entityId,
            Action = action,
            ProjectKey = scope.Project,
            EnvironmentKey = scope.Environment,
            FlagKey = scope.Flag,
            Before = before is null ? null : JsonSerializer.Serialize(before, SnapshotOptions),
            After = after is null ? null : JsonSerializer.Serialize(after, SnapshotOptions),
        });
}
