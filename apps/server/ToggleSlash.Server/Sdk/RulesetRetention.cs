using System.Globalization;
using ToggleSlash.Server.Data;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.DependencyInjection.Extensions;

namespace ToggleSlash.Server.Sdk;

/// <summary>How long a replaced ruleset snapshot is kept. 0 keeps every snapshot, so the service does nothing.</summary>
public sealed record RulesetRetentionSettings(int Days)
{
    public const string Setting = "TOGGLESLASH_RULESET_RETENTION_DAYS";

    private const int DefaultDays = 90;

    /// <summary>Unset takes the default. Anything but a non-negative integer stops the server, naming the variable.</summary>
    public static RulesetRetentionSettings From(IConfiguration configuration)
    {
        if (configuration[Setting] is not { } text)
        {
            return new RulesetRetentionSettings(DefaultDays);
        }
        return int.TryParse(text, NumberStyles.None, CultureInfo.InvariantCulture, out var days)
            ? new RulesetRetentionSettings(days)
            : throw new InvalidOperationException($"{Setting} must be a whole number of days, 0 or more (0 keeps every ruleset).");
    }
}

/// <summary>
/// Deletes ruleset snapshots nothing new can point at: once at startup, then once an hour. Per environment, a snapshot
/// goes when its successor (the next one by <c>first_served_at</c>, ties broken by hash) was first served before the
/// retention window, so age counts from when it stopped being the newest, and its hash is not the environment's current
/// ruleset (a rollback makes an old body current again without moving <c>first_served_at</c>). The newest snapshot has
/// no successor and is never deleted. A rollback that lands between reading the current ruleset and the delete is repaired
/// by recording the current snapshot again when the delete had removed it. Housekeeping, not an admin write: no audit event.
/// </summary>
public sealed class RulesetRetention(
    IServiceScopeFactory scopes, TimeProvider time, RulesetRetentionSettings settings, ILogger<RulesetRetention> logger) : BackgroundService
{
    private static readonly TimeSpan Interval = TimeSpan.FromHours(1);

    /// <summary>For tests, null in production: awaited with the environment id once the doomed list is known, before the delete begins.</summary>
    public Func<long, CancellationToken, Task>? BeforeDelete { get; init; }

    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        if (settings.Days == 0)
        {
            return;
        }
        using var timer = new PeriodicTimer(Interval, time);
        do
        {
            // A BackgroundService that throws takes the host down, and housekeeping must not.
            try
            {
                await Sweep(stoppingToken);
            }
            catch (Exception exception) when (exception is not OperationCanceledException)
            {
                logger.LogWarning(exception, "A ruleset retention sweep failed; the snapshots were left as they are.");
            }
        }
        while (await timer.WaitForNextTickAsync(stoppingToken));
    }

    /// <summary>One sweep, as the timer runs it. Returns the number of snapshots removed; with the setting at 0 it removes none.</summary>
    public async Task<int> Sweep(CancellationToken cancellation)
    {
        if (settings.Days == 0)
        {
            return 0;
        }
        var now = time.GetUtcNow().UtcDateTime;
        // A window longer than the calendar reaches back past every date there is: nothing is older than it.
        var cutoff = settings.Days >= (now - DateTime.MinValue).TotalDays ? DateTime.MinValue : now.AddDays(-settings.Days);
        long[] environments;
        using (var scope = scopes.CreateScope())
        {
            environments = await scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>().Environments
                .AsNoTracking().Select(e => e.Id).ToArrayAsync(cancellation);
        }

        var removed = 0;
        foreach (var environment in environments)
        {
            try
            {
                removed += await SweepEnvironment(environment, cutoff, cancellation);
            }
            catch (Exception exception) when (exception is not OperationCanceledException)
            {
                logger.LogWarning(exception, "Could not prune the ruleset snapshots of environment {EnvironmentId}; they were left as they are.", environment);
            }
        }
        logger.LogInformation("Ruleset retention removed {Removed} snapshots older than {Days} days.", removed, settings.Days);
        return removed;
    }

    private async Task<int> SweepEnvironment(long environmentId, DateTime cutoff, CancellationToken cancellation)
    {
        // A scope of its own: the reader opens a transaction on its context, and the delete needs a clean one.
        using var scope = scopes.CreateScope();
        var database = scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>();

        var rows = (await database.RulesetSnapshots.AsNoTracking()
            .Where(s => s.EnvironmentId == environmentId)
            .Select(s => new { s.Hash, s.FirstServedAt })
            .ToListAsync(cancellation))
            .OrderBy(s => s.FirstServedAt).ThenBy(s => s.Hash, StringComparer.Ordinal)
            .ToList();

        // Row i has row i + 1 as its successor, so the newest row is never a candidate.
        var candidates = Enumerable.Range(0, Math.Max(rows.Count - 1, 0))
            .Where(i => rows[i + 1].FirstServedAt < cutoff)
            .Select(i => rows[i].Hash)
            .ToList();
        if (candidates.Count == 0)
        {
            return 0;
        }

        var current = (await scope.ServiceProvider.GetRequiredService<RulesetReader>().Read(environmentId, cancellation)).Hash;
        var doomed = candidates.Where(hash => hash != current).ToList();
        if (doomed.Count == 0)
        {
            return 0;
        }

        await (BeforeDelete?.Invoke(environmentId, cancellation) ?? Task.CompletedTask);

        int removed;
        await using (var transaction = await database.Database.BeginTransactionAsync(cancellation))
        {
            removed = await database.RulesetSnapshots
                .Where(s => s.EnvironmentId == environmentId && doomed.Contains(s.Hash))
                .ExecuteDeleteAsync(cancellation);
            await transaction.CommitAsync(cancellation);
        }

        // A rollback between the read above and the delete can make a doomed hash current: SDKs were answered 200 for
        // it while its row still existed, so nothing recorded it again. Put the current snapshot back if it was deleted.
        var reader = scope.ServiceProvider.GetRequiredService<RulesetReader>();
        var after = await reader.Read(environmentId, cancellation);
        if (doomed.Contains(after.Hash))
        {
            await reader.RecordServed(environmentId, after, cancellation);
        }
        return removed;
    }
}

public static class RulesetRetentionSetup
{
    public static IServiceCollection AddRulesetRetention(this IServiceCollection services)
    {
        services.TryAddSingleton(TimeProvider.System);
        services.AddSingleton(provider => RulesetRetentionSettings.From(provider.GetRequiredService<IConfiguration>()));
        services.AddHostedService<RulesetRetention>();
        return services;
    }
}
