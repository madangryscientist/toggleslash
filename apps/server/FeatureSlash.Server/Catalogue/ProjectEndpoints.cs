using System.Security.Claims;
using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Catalogue;

public static class ProjectEndpoints
{
    public sealed record CreateProjectRequest(string? Key, string? Name);

    public sealed record ProjectResponse(string Key, string Name, DateTime CreatedAt);

    public static void MapProjectEndpoints(this IEndpointRouteBuilder routes)
    {
        var projects = routes.MapGroup("/api/v1/projects")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Projects");

        projects.MapPost("", Create)
            .WithMetadata(new RootOnlyRoute())
            .WithName("createProject")
            .Produces<ProjectResponse>(StatusCodes.Status201Created)
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        projects.MapGet("", List)
            .WithMetadata(new OwnProjectListRoute())
            .WithName("listProjects")
            .Produces<ProjectResponse[]>();
        projects.MapGet("{project}", Read)
            .WithName("getProject")
            .Produces<ProjectResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    private static async Task<IResult> Create(CreateProjectRequest request, FeatureSlashDbContext database, CancellationToken cancellation)
    {
        if ((Validation.Key(request.Key, "key") ?? Validation.Name(request.Name)) is { } problem)
        {
            return Failures.BadRequest(problem);
        }
        if (await database.Projects.AnyAsync(p => p.Key == request.Key, cancellation))
        {
            return Failures.Conflict($"A project with key \"{request.Key}\" already exists.");
        }

        var project = new Project { Key = request.Key!, Name = request.Name!.Trim(), CreatedAt = DateTime.UtcNow };
        database.Projects.Add(project);
        database.Record(AuditEntity.Project, project.Key, AuditAction.Created, new AuditScope(project.Key), null, Describe(project));
        try
        {
            await database.SaveChangesAsync(cancellation);
        }
        catch (DbUpdateException exception) when (Failures.IsUniqueViolation(exception))
        {
            return Failures.Conflict($"A project with key \"{request.Key}\" already exists.");
        }
        return Results.Created($"/api/v1/projects/{project.Key}", Describe(project));
    }

    /// <summary>Every project for the root token; a scoped token sees only its own.</summary>
    private static async Task<IResult> List(ClaimsPrincipal user, FeatureSlashDbContext database, CancellationToken cancellation)
    {
        var visible = database.Projects.AsNoTracking();
        if (ServerAuth.TokenProject(user) is { } own)
        {
            visible = visible.Where(p => p.Key == own);
        }
        var projects = await visible.OrderBy(p => p.Id).ToListAsync(cancellation);
        return Results.Json(projects.Select(Describe));
    }

    private static async Task<IResult> Read(string project, FeatureSlashDbContext database, CancellationToken cancellation) =>
        await database.Project(project, cancellation) is { } found ? Results.Json(Describe(found)) : Failures.ProjectNotFound();

    private static ProjectResponse Describe(Project project) => new(project.Key, project.Name, project.CreatedAt);
}
