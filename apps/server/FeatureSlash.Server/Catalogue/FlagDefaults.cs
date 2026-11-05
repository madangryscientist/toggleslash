using FeatureSlash.Server.Data;

namespace FeatureSlash.Server.Catalogue;

/// <summary>What a flag's config row starts as in an environment: off, serving the same variations everywhere.</summary>
internal static class FlagDefaults
{
    /// <summary>A flag has at least 2 variations, so index 1 always exists. For [true, false] that is false.</summary>
    public const int OffVariation = 1;

    public const int FallthroughVariation = 0;

    public static FlagEnvironmentConfig NewConfig(long flagId, long environmentId, int offVariation, int fallthroughVariation) => new()
    {
        FlagId = flagId,
        EnvironmentId = environmentId,
        On = false,
        OffVariation = offVariation,
        Fallthrough = $$"""{"variation":{{fallthroughVariation}}}""",
        Version = 1,
    };
}
