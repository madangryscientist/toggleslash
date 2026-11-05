using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Catalogue;

public static class EnvironmentEndpoints
{
    public sealed record CreateEnvironmentRequest(string? Key, string? Name);

    public sealed record EnvironmentResponse(string Key, string Name, DateTime CreatedAt);

    public static void MapEnvironmentEndpoints(this IEndpointRouteBuilder routes)
    {
        var environments = routes.MapGroup("/api/v1/projects/{project}/environments")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Environments");

        environments.MapPost("", Create)
            .WithName("createEnvironment")
            .Produces<EnvironmentResponse>(StatusCodes.Status201Created)
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        environments.MapGet("", List)
            .WithName("listEnvironments")
            .Produces<EnvironmentResponse[]>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        environments.MapGet("{env}", Read)
            .WithName("getEnvironment")
            .Produces<EnvironmentResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    /// <summary>
    /// A new environment gets an off config row for every flag the project already has, archived ones
    /// included, so "every flag has a config in every environment" holds whichever was created first.
    /// </summary>
    private static async Task<IResult> Create(string project, CreateEnvironmentRequest request, FeatureSlashDbContext database)
    {
        if ((Validation.Key(request.Key, "key") ?? Validation.Name(request.Name)) is { } problem)
        {
            return Failures.BadRequest(problem);
        }
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, request.Key!) is not null)
        {
            return Taken(request.Key!);
        }

        var environment = new ProjectEnvironment
        {
            ProjectId = owner.Id,
            Key = request.Key!,
            Name = request.Name!.Trim(),
            CreatedAt = DateTime.UtcNow,
        };
        await using var transaction = await database.Database.BeginTransactionAsync();
        database.Environments.Add(environment);
        try
        {
            await database.SaveChangesAsync();
            var flagIds = await database.Flags.Where(f => f.ProjectId == owner.Id).Select(f => f.Id).ToListAsync();
            database.FlagEnvironmentConfigs.AddRange(flagIds.Select(id =>
                FlagDefaults.NewConfig(id, environment.Id, FlagDefaults.OffVariation, FlagDefaults.FallthroughVariation)));
            await database.SaveChangesAsync();
            await transaction.CommitAsync();
        }
        catch (DbUpdateException exception) when (Failures.IsUniqueViolation(exception))
        {
            return Taken(request.Key!);
        }
        return Results.Created($"/api/v1/projects/{owner.Key}/environments/{environment.Key}", Describe(environment));
    }

    private static async Task<IResult> List(string project, FeatureSlashDbContext database)
    {
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        var environments = await database.Environments.AsNoTracking()
            .Where(e => e.ProjectId == owner.Id)
            .OrderBy(e => e.Id)
            .ToListAsync();
        return Results.Json(environments.Select(Describe));
    }

    private static async Task<IResult> Read(string project, string env, FeatureSlashDbContext database)
    {
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        return await database.Environment(owner.Id, env) is { } found
            ? Results.Json(Describe(found))
            : Failures.EnvironmentNotFound();
    }

    private static IResult Taken(string key) =>
        Failures.Conflict($"An environment with key \"{key}\" already exists in this project.");

    private static EnvironmentResponse Describe(ProjectEnvironment environment) =>
        new(environment.Key, environment.Name, environment.CreatedAt);
}
