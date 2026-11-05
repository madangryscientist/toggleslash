namespace FeatureSlash.Server.Data;

// A property typed string and documented as JSON is stored as text, in the shapes docs/evaluation.md
// defines. The database does not read inside it. Every timestamp is UTC.

public sealed class Project
{
    public long Id { get; set; }
    public required string Key { get; set; }
    public required string Name { get; set; }
    public required DateTime CreatedAt { get; set; }
}

/// <summary>An environment of a project. Named so it does not shadow <see cref="System.Environment"/>.</summary>
public sealed class ProjectEnvironment
{
    public long Id { get; set; }
    public required long ProjectId { get; set; }
    public required string Key { get; set; }
    public required string Name { get; set; }
    public required DateTime CreatedAt { get; set; }
}

public sealed class Flag
{
    public long Id { get; set; }
    public required long ProjectId { get; set; }
    public required string Key { get; set; }
    public required string Name { get; set; }
    public string Description { get; set; } = "";

    /// <summary>JSON array of any JSON values.</summary>
    public required string Variations { get; set; }

    /// <summary>The bucket hash's salt. Not per environment in docs/evaluation.md, so it lives here.</summary>
    public required string Salt { get; set; }

    public bool Temporary { get; set; }
    public string? Owner { get; set; }

    /// <summary>JSON array of strings.</summary>
    public string Tags { get; set; } = "[]";

    public required DateTime CreatedAt { get; set; }
    public DateTime? ArchivedAt { get; set; }
}

/// <summary>The fields of a flag that differ per environment.</summary>
public sealed class FlagEnvironmentConfig
{
    public long Id { get; set; }
    public required long FlagId { get; set; }
    public required long EnvironmentId { get; set; }
    public bool On { get; set; }
    public int? OffVariation { get; set; }

    /// <summary>JSON array of targets.</summary>
    public string Targets { get; set; } = "[]";

    /// <summary>JSON array of prerequisites.</summary>
    public string Prerequisites { get; set; } = "[]";

    /// <summary>JSON array of rules.</summary>
    public string Rules { get; set; } = "[]";

    /// <summary>JSON serve object.</summary>
    public required string Fallthrough { get; set; }

    public long Version { get; set; }
}

public sealed class Segment
{
    public long Id { get; set; }
    public required long ProjectId { get; set; }
    public required long EnvironmentId { get; set; }
    public required string Key { get; set; }
    public required string Salt { get; set; }

    /// <summary>JSON array of context keys.</summary>
    public string Included { get; set; } = "[]";

    /// <summary>JSON array of context keys.</summary>
    public string Excluded { get; set; } = "[]";

    /// <summary>JSON array of segment rules.</summary>
    public string Rules { get; set; } = "[]";

    /// <summary>Starts at 1 and goes up by one on every save. Never served in the ruleset.</summary>
    public long Version { get; set; }
}

public enum ApiKeyKind
{
    Server,
    Client,
}

/// <summary>An SDK key. Only its hash is stored; the plain key is shown once, when it is created.</summary>
public sealed class ApiKey
{
    public long Id { get; set; }
    public required long EnvironmentId { get; set; }
    public required ApiKeyKind Kind { get; set; }
    public required string Name { get; set; }

    /// <summary>SHA-256 of the plain key, lowercase hex.</summary>
    public required string Hash { get; set; }

    /// <summary>The key's first characters, for display.</summary>
    public required string Prefix { get; set; }

    public required DateTime CreatedAt { get; set; }
    public DateTime? RevokedAt { get; set; }
}

public enum AdminTokenPermission
{
    Read,
    Write,
}

/// <summary>
/// A scoped admin API credential: one project, read or write, always with an expiry. Only its hash is stored; the
/// plain token is shown once, when it is created.
/// </summary>
public sealed class AdminToken
{
    public long Id { get; set; }
    public required long ProjectId { get; set; }
    public required string Name { get; set; }
    public required AdminTokenPermission Permission { get; set; }

    /// <summary>SHA-256 of the plain token, lowercase hex.</summary>
    public required string Hash { get; set; }

    /// <summary>The token's first characters, for display.</summary>
    public required string Prefix { get; set; }

    public required DateTime CreatedAt { get; set; }
    public required DateTime ExpiresAt { get; set; }
    public DateTime? RevokedAt { get; set; }
}

/// <summary>Who changed what. No foreign keys: an event outlives the entity it describes.</summary>
public sealed class AuditEvent
{
    public long Id { get; set; }
    public required DateTime OccurredAt { get; set; }
    public required string Actor { get; set; }
    public required string EntityType { get; set; }
    public required string EntityId { get; set; }
    public required string Action { get; set; }

    // Where the event belongs, as the keys the API addresses things by. Keys are fixed once created, so they
    // stay true after the entity is gone, and the audit routes filter on them without a join.
    public required string ProjectKey { get; set; }
    public string? EnvironmentKey { get; set; }
    public string? FlagKey { get; set; }

    /// <summary>JSON; null when the entity did not exist before.</summary>
    public string? Before { get; set; }

    /// <summary>JSON; null when the entity no longer exists after.</summary>
    public string? After { get; set; }
}

public sealed class FlagUsageDaily
{
    public required long EnvironmentId { get; set; }
    public required long FlagId { get; set; }
    public required int Variation { get; set; }
    public required DateOnly Day { get; set; }
    public long Count { get; set; }
}
