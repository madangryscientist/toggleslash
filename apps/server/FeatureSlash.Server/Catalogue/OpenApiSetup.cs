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
                    Description = "The FeatureSlash server's admin API and SDK read API. Every route under /api/v1 takes `Authorization: Bearer <admin token>`, or a scoped token where its project and permission allow it. " +
                        "GET /sdk/v1/ruleset and GET /sdk/v1/stream take a server SDK key; POST /sdk/v1/evaluate takes a server or a client SDK key.",
                };
                // MapOpenApi fills this from the request's host, so the same server would give a different file per caller.
                document.Servers = null;
                document.Components ??= new OpenApiComponents();
                document.Components.SecuritySchemes ??= new Dictionary<string, IOpenApiSecurityScheme>();
                document.Components.SecuritySchemes[BearerScheme] = new OpenApiSecurityScheme
                {
                    Type = SecuritySchemeType.Http,
                    Scheme = "bearer",
                    Description = "The admin token (read from FEATURESLASH_ADMIN_TOKEN) or a scoped token on /api/v1, or an SDK key on /sdk/v1.",
                };
                return Task.CompletedTask;
            });
            options.AddOperationTransformer((operation, context, _) =>
            {
                var gated = context.Description.ActionDescriptor.EndpointMetadata
                    .OfType<IAuthorizeData>()
                    .Any(data => data.Policy is ServerAuth.AdminPolicy or ServerAuth.ServerKeyPolicy or ServerAuth.SdkKeyPolicy);
                if (gated)
                {
                    operation.Responses ??= [];
                    operation.Responses["429"] = new OpenApiResponse
                    {
                        Description = "Too many requests: this IP has sent too many wrong credentials, or this SDK key has reached a limit.",
                        Headers = new Dictionary<string, IOpenApiHeader>
                        {
                            ["Retry-After"] = new OpenApiHeader
                            {
                                Description = "Whole seconds to wait, rounded up.",
                                Schema = new OpenApiSchema { Type = JsonSchemaType.Integer },
                            },
                        },
                        Content = new Dictionary<string, OpenApiMediaType>
                        {
                            ["application/json"] = new OpenApiMediaType { Schema = new OpenApiSchemaReference(nameof(ApiError), context.Document) },
                        },
                    };
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
