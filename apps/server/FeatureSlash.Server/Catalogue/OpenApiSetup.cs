using FeatureSlash.Server.Auth;
using Microsoft.AspNetCore.Authorization;
using Microsoft.OpenApi;

namespace FeatureSlash.Server.Catalogue;

/// <summary>The built-in OpenAPI document at <c>/openapi/v1.json</c>, and what makes it the same on every machine.</summary>
public static class OpenApiSetup
{
    public const string Path = "/openapi/v1.json";

    private const string BearerScheme = "bearer";

    public static IServiceCollection AddServerOpenApi(this IServiceCollection services) =>
        services.AddOpenApi("v1", options =>
        {
            options.AddDocumentTransformer((document, _, _) =>
            {
                document.Info = new OpenApiInfo
                {
                    Title = "FeatureSlash API",
                    Version = "v1",
                    Description = "The FeatureSlash server's admin API. Every route under /api/v1 takes `Authorization: Bearer <admin token>`.",
                };
                // MapOpenApi fills this from the request's host, so the same server would give a different file per caller.
                document.Servers = null;
                document.Components ??= new OpenApiComponents();
                document.Components.SecuritySchemes ??= new Dictionary<string, IOpenApiSecurityScheme>();
                document.Components.SecuritySchemes[BearerScheme] = new OpenApiSecurityScheme
                {
                    Type = SecuritySchemeType.Http,
                    Scheme = "bearer",
                    Description = "The admin token, which the server reads from FEATURESLASH_ADMIN_TOKEN.",
                };
                return Task.CompletedTask;
            });
            options.AddOperationTransformer((operation, context, _) =>
            {
                var admin = context.Description.ActionDescriptor.EndpointMetadata
                    .OfType<IAuthorizeData>()
                    .Any(data => data.Policy == ServerAuth.AdminPolicy);
                if (admin)
                {
                    operation.Security ??= [];
                    operation.Security.Add(new OpenApiSecurityRequirement
                    {
                        [new OpenApiSecuritySchemeReference(BearerScheme, context.Document)] = [],
                    });
                }
                return Task.CompletedTask;
            });
        });
}
