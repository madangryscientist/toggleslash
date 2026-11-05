using System.Text.RegularExpressions;
using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Storage.ValueConversion;

namespace FeatureSlash.Server.Data;

public sealed partial class FeatureSlashDbContext(DbContextOptions<FeatureSlashDbContext> options)
    : DbContext(options)
{
    public DbSet<Project> Projects => Set<Project>();
    public DbSet<ProjectEnvironment> Environments => Set<ProjectEnvironment>();
    public DbSet<Flag> Flags => Set<Flag>();
    public DbSet<FlagEnvironmentConfig> FlagEnvironmentConfigs => Set<FlagEnvironmentConfig>();
    public DbSet<Segment> Segments => Set<Segment>();
    public DbSet<ApiKey> ApiKeys => Set<ApiKey>();
    public DbSet<AuditEvent> AuditEvents => Set<AuditEvent>();
    public DbSet<FlagUsageDaily> FlagUsageDaily => Set<FlagUsageDaily>();

    protected override void ConfigureConventions(ModelConfigurationBuilder configurationBuilder)
    {
        // SQLite keeps a DateTime as text and hands it back with no kind. Every timestamp here is UTC.
        configurationBuilder.Properties<DateTime>().HaveConversion<UtcDateTimeConverter>();
    }

    protected override void OnModelCreating(ModelBuilder modelBuilder)
    {
        modelBuilder.Entity<Project>(project =>
        {
            project.ToTable("projects");
            project.HasIndex(p => p.Key).IsUnique();
        });

        modelBuilder.Entity<ProjectEnvironment>(environment =>
        {
            environment.ToTable("environments");
            environment.HasOne<Project>().WithMany().HasForeignKey(e => e.ProjectId);
            environment.HasIndex(e => new { e.ProjectId, e.Key }).IsUnique();
        });

        modelBuilder.Entity<Flag>(flag =>
        {
            flag.ToTable("flags");
            flag.HasOne<Project>().WithMany().HasForeignKey(f => f.ProjectId);
            flag.HasIndex(f => new { f.ProjectId, f.Key }).IsUnique();
        });

        modelBuilder.Entity<FlagEnvironmentConfig>(config =>
        {
            config.ToTable("flag_environment_configs");
            config.HasOne<Flag>().WithMany().HasForeignKey(c => c.FlagId);
            config.HasOne<ProjectEnvironment>().WithMany().HasForeignKey(c => c.EnvironmentId);
            config.HasIndex(c => new { c.FlagId, c.EnvironmentId }).IsUnique();
        });

        modelBuilder.Entity<Segment>(segment =>
        {
            segment.ToTable("segments");
            segment.HasOne<Project>().WithMany().HasForeignKey(s => s.ProjectId);
            segment.HasOne<ProjectEnvironment>().WithMany().HasForeignKey(s => s.EnvironmentId);
            segment.HasIndex(s => new { s.EnvironmentId, s.Key }).IsUnique();
        });

        modelBuilder.Entity<ApiKey>(key =>
        {
            key.ToTable("api_keys", table =>
                table.HasCheckConstraint("ck_api_keys_kind", "kind IN ('server', 'client')"));
            key.HasOne<ProjectEnvironment>().WithMany().HasForeignKey(k => k.EnvironmentId);
            key.Property(k => k.Kind).HasConversion(
                kind => kind == ApiKeyKind.Server ? "server" : "client",
                kind => kind == "server" ? ApiKeyKind.Server : ApiKeyKind.Client);
            key.HasIndex(k => k.Hash).IsUnique();
        });

        modelBuilder.Entity<AuditEvent>(audit =>
        {
            audit.ToTable("audit_events");
            // The audit routes page newest first within a project, and a flag's history within that.
            audit.HasIndex(a => new { a.ProjectKey, a.Id });
            audit.HasIndex(a => new { a.ProjectKey, a.FlagKey, a.Id });
        });

        modelBuilder.Entity<FlagUsageDaily>(usage =>
        {
            usage.ToTable("flag_usage_daily");
            usage.HasKey(u => new { u.EnvironmentId, u.FlagId, u.Variation, u.Day });
            usage.HasOne<ProjectEnvironment>().WithMany().HasForeignKey(u => u.EnvironmentId);
            usage.HasOne<Flag>().WithMany().HasForeignKey(u => u.FlagId);
        });

        foreach (var property in modelBuilder.Model.GetEntityTypes().SelectMany(e => e.GetProperties()))
        {
            property.SetColumnName(WordStart().Replace(property.Name, "_$0").ToLowerInvariant());
        }
    }

    [GeneratedRegex("(?<!^)[A-Z]")]
    private static partial Regex WordStart();

    private sealed class UtcDateTimeConverter()
        : ValueConverter<DateTime, DateTime>(v => v.ToUniversalTime(), v => DateTime.SpecifyKind(v, DateTimeKind.Utc));
}
