using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Diagnostics;

namespace ToggleSlash.Server.Data;

public static class ServerDatabase
{
    public const string PathSetting = "TOGGLESLASH_DB_PATH";

    /// <summary>Relative to the process's working directory. <c>*.db</c> is git-ignored.</summary>
    private const string DefaultPath = "toggleslash.db";

    private static string PathFrom(IConfiguration configuration) =>
        configuration[PathSetting] is { Length: > 0 } path ? path : DefaultPath;

    public static IServiceCollection AddServerDatabase(this IServiceCollection services) =>
        services.AddDbContext<ToggleSlashDbContext>((provider, options) =>
            options
                .UseSqlite(new SqliteConnectionStringBuilder
                {
                    DataSource = PathFrom(provider.GetRequiredService<IConfiguration>()),
                }.ToString())
                // MigrateServerDatabase's transaction is deliberate; see there.
                .ConfigureWarnings(warnings => warnings.Ignore(RelationalEventId.MigrationsUserTransactionWarning)));

    /// <summary>
    /// Applies every pending migration in one transaction, so a failure leaves the file as it was. With
    /// none pending it changes nothing. On its own <c>Migrate</c> commits per migration, which would
    /// leave a half-upgraded file behind. The cost of the outer transaction is that a migration SQLite
    /// has to run outside one (a table rebuild) fails here, and migrations are additive anyway.
    /// </summary>
    public static void MigrateServerDatabase(this IServiceProvider services)
    {
        using var scope = services.CreateScope();
        var database = scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>().Database;

        // EF sets this only when it creates the file itself, and the transaction below creates it first.
        database.ExecuteSqlRaw("PRAGMA journal_mode = 'wal'");

        using var transaction = database.BeginTransaction();
        database.Migrate();
        transaction.Commit();
    }
}
