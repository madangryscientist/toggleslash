using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Data;
using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Catalogue;

/// <summary>The error answers the catalogue routes share, so every route says the same thing the same way.</summary>
internal static class Failures
{
    public static IResult BadRequest(string message) =>
        ApiError.Result(StatusCodes.Status400BadRequest, "bad_request", message);

    public static IResult Conflict(string message) =>
        ApiError.Result(StatusCodes.Status409Conflict, "conflict", message);

    public static IResult VersionConflict(string message) =>
        ApiError.Result(StatusCodes.Status409Conflict, "version_conflict", message);

    public static IResult ProjectNotFound() =>
        ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No such project.");

    public static IResult EnvironmentNotFound() =>
        ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No such environment.");

    public static IResult FlagNotFound() =>
        ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No such flag.");

    /// <summary>
    /// A unique index refused the write: the key was free when checked and taken before the insert. The
    /// pre-check gives the friendly message; this catches the race, so it is still a 409 and not a 500.
    /// Only a unique or primary-key violation (extended codes 2067 and 1555): any other constraint failure,
    /// such as the audit event refusing to write, is the server's fault and stays a 500.
    /// </summary>
    public static bool IsUniqueViolation(DbUpdateException exception) =>
        exception.InnerException is SqliteException { SqliteExtendedErrorCode: 2067 or 1555 };
}

internal static class Lookups
{
    public static Task<Project?> Project(this FeatureSlashDbContext database, string key) =>
        database.Projects.AsNoTracking().SingleOrDefaultAsync(p => p.Key == key);

    public static Task<ProjectEnvironment?> Environment(this FeatureSlashDbContext database, long projectId, string key) =>
        database.Environments.AsNoTracking().SingleOrDefaultAsync(e => e.ProjectId == projectId && e.Key == key);
}
