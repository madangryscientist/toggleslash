namespace FeatureSlash.Server.Limits;

/// <summary>
/// Every rate limit's state, in this process's memory: token buckets per key and per IP, and open streams per key.
/// One server process owns one database, so that is enough; a restart resets the counts. Built by hand because the
/// built-in ASP.NET limiter's timers cannot use a fake clock. It holds a key's id and a grouped IP, stores neither
/// and logs nothing.
/// </summary>
public sealed class RequestLimiter(TimeProvider clock, RateLimitSettings settings)
{
    /// <summary>A refused stream has no refill to wait for: a slot comes back when another stream ends.</summary>
    public const long StreamRetryAfterSeconds = 30;

    private readonly Lock gate = new();
    private readonly Buckets<long> evaluate = new(clock, settings.EvaluatePerMinute, settings.MaxTrackedKeys);
    private readonly Buckets<long> events = new(clock, settings.EventsPerMinute, settings.MaxTrackedKeys);
    private readonly Buckets<string> authFailures = new(clock, settings.AuthFailuresPerMinute, settings.MaxTrackedIps);
    private readonly Dictionary<long, int> streams = [];

    /// <summary>Takes one request from the key's bucket for this route. False when it is empty, with the seconds to wait.</summary>
    public bool TryTake(KeyLimit limit, long keyId, out long retryAfterSeconds)
    {
        lock (gate)
        {
            var buckets = limit == KeyLimit.Evaluate ? evaluate : events;
            var bucket = buckets.GetOrAdd(keyId);
            if (bucket.IsEmpty(out retryAfterSeconds))
            {
                return false;
            }
            bucket.Spend();
            return true;
        }
    }

    /// <summary>True when this IP has used its failures, so its credential must not even be checked.</summary>
    public bool IsLockedOut(string ip, out long retryAfterSeconds)
    {
        lock (gate)
        {
            // Only a failure starts tracking an IP, so correct credentials cost no memory here.
            if (authFailures.Find(ip) is { } bucket)
            {
                return bucket.IsEmpty(out retryAfterSeconds);
            }
            retryAfterSeconds = 0;
            return false;
        }
    }

    /// <summary>
    /// Counts a wrong credential. Concurrent guesses can all pass <see cref="IsLockedOut"/> before the first of them
    /// fails, so this can overdraw the bucket: the IP then waits that off, and the rate holds over time.
    /// </summary>
    public void RecordAuthFailure(string ip)
    {
        lock (gate)
        {
            authFailures.GetOrAdd(ip).Spend();
        }
    }

    /// <summary>
    /// One of the key's stream slots, released by disposing it; null when none is free. A slot is live state, so it
    /// cannot be evicted: past the cap on tracked keys, a key with no open stream is refused instead.
    /// </summary>
    public IDisposable? TryOpenStream(long keyId)
    {
        lock (gate)
        {
            var open = streams.GetValueOrDefault(keyId);
            if (open >= settings.StreamsPerKey || (open == 0 && streams.Count >= settings.MaxTrackedKeys))
            {
                return null;
            }
            streams[keyId] = open + 1;
            return new StreamSlot(this, keyId);
        }
    }

    /// <summary>How many IPs and keys are held: what the caps bound.</summary>
    public (int Ips, int EvaluateKeys, int EventKeys, int StreamKeys) Tracked
    {
        get
        {
            lock (gate)
            {
                return (authFailures.Count, evaluate.Count, events.Count, streams.Count);
            }
        }
    }

    private void CloseStream(long keyId)
    {
        lock (gate)
        {
            var open = streams[keyId] - 1;
            if (open == 0)
            {
                streams.Remove(keyId);
            }
            else
            {
                streams[keyId] = open;
            }
        }
    }

    private sealed class StreamSlot(RequestLimiter limiter, long keyId) : IDisposable
    {
        public void Dispose() => limiter.CloseStream(keyId);
    }

    /// <summary>A table of buckets holding at most <paramref name="cap"/>; the one touched longest ago goes first.</summary>
    private sealed class Buckets<TKey>(TimeProvider clock, int perMinute, int cap) where TKey : notnull
    {
        private readonly Dictionary<TKey, LinkedListNode<(TKey Key, Bucket Bucket)>> table = [];
        private readonly LinkedList<(TKey Key, Bucket Bucket)> byUse = new();

        public int Count => table.Count;

        public Bucket? Find(TKey key)
        {
            if (!table.TryGetValue(key, out var node))
            {
                return null;
            }
            byUse.Remove(node);
            byUse.AddFirst(node);
            return node.Value.Bucket;
        }

        public Bucket GetOrAdd(TKey key)
        {
            if (Find(key) is { } found)
            {
                return found;
            }
            if (table.Count >= cap)
            {
                table.Remove(byUse.Last!.Value.Key);
                byUse.RemoveLast();
            }
            var bucket = new Bucket(clock, perMinute);
            table[key] = byUse.AddFirst((key, bucket));
            return bucket;
        }
    }

    /// <summary>
    /// A token bucket holding a minute's allowance, in whole numbers so a <c>Retry-After</c> is exact: one token is
    /// <see cref="TimeSpan.TicksPerMinute"/> units, and each tick of time adds <paramref name="perMinute"/> units.
    /// </summary>
    private sealed class Bucket(TimeProvider clock, int perMinute)
    {
        private readonly long capacity = perMinute * TimeSpan.TicksPerMinute;
        private long units = perMinute * TimeSpan.TicksPerMinute;
        private long refilledAt = clock.GetTimestamp();

        /// <summary>Whether a whole token is missing, and if so how many whole seconds, rounded up, until there is one.</summary>
        public bool IsEmpty(out long retryAfterSeconds)
        {
            var now = clock.GetTimestamp();
            var earned = (Int128)clock.GetElapsedTime(refilledAt, now).Ticks * perMinute;
            units = (long)Int128.Min(units + earned, capacity);
            refilledAt = now;

            var missing = TimeSpan.TicksPerMinute - units;
            if (missing <= 0)
            {
                retryAfterSeconds = 0;
                return false;
            }
            var ticks = (missing + perMinute - 1) / perMinute;
            retryAfterSeconds = (ticks + TimeSpan.TicksPerSecond - 1) / TimeSpan.TicksPerSecond;
            return true;
        }

        public void Spend()
        {
            IsEmpty(out _);
            units -= TimeSpan.TicksPerMinute;
        }
    }
}

/// <summary>The per-key limits, one bucket per key per route.</summary>
public enum KeyLimit
{
    Evaluate,
    Events,
}
