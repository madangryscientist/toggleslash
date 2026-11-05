using FeatureSlash.Evaluation;

/// <summary>The pins in <c>fixtures/evaluation/buckets.json</c>, written from an independent SHA-256.</summary>
public sealed class BucketTests
{
    public static IEnumerable<object[]> Pins() =>
        Fixtures.Buckets().EnumerateArray().Select((pin, position) => new object[] { position });

    [Theory]
    [MemberData(nameof(Pins))]
    public void Bucket_matches_the_pin(int position)
    {
        var pin = Fixtures.Buckets().EnumerateArray().ElementAt(position);

        var bucket = Bucketing.Bucket(
            pin.GetProperty("flagKey").GetString()!,
            pin.GetProperty("salt").GetString()!,
            pin.GetProperty("contextKey").GetString()!);

        Assert.Equal(pin.GetProperty("bucket").GetInt32(), bucket);
    }
}
