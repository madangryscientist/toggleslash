using System.Threading.Channels;

namespace ToggleSlash.Server.Sdk;

/// <summary>
/// Tells the streams of an environment that its ruleset may have changed. A write calls <see cref="Changed"/>
/// after it commits, so a stream that reads on the signal sees the write. It lives in this process only
/// (docs/architecture.md): a second server process would not hear it.
/// </summary>
public sealed class RulesetNotifier
{
    private readonly Dictionary<long, HashSet<Subscription>> subscribers = [];

    /// <summary>Streams open right now, across every environment.</summary>
    public int SubscriberCount
    {
        get
        {
            lock (subscribers)
            {
                return subscribers.Values.Sum(environment => environment.Count);
            }
        }
    }

    public Subscription Subscribe(long environmentId)
    {
        var subscription = new Subscription(this, environmentId);
        lock (subscribers)
        {
            if (!subscribers.TryGetValue(environmentId, out var environment))
            {
                subscribers[environmentId] = environment = [];
            }
            environment.Add(subscription);
        }
        return subscription;
    }

    /// <summary>Never waits on a subscriber, however slowly it reads: a write is not held up by a stream.</summary>
    public void Changed(long environmentId)
    {
        lock (subscribers)
        {
            if (subscribers.TryGetValue(environmentId, out var environment))
            {
                foreach (var subscription in environment)
                {
                    subscription.Signal();
                }
            }
        }
    }

    private void Remove(Subscription subscription, long environmentId)
    {
        lock (subscribers)
        {
            if (subscribers.TryGetValue(environmentId, out var environment) && environment.Remove(subscription) && environment.Count == 0)
            {
                subscribers.Remove(environmentId);
            }
        }
    }

    /// <summary>
    /// Holds one pending signal and drops the rest, so a burst of writes is one wake-up. The signal names no
    /// ETag: the reader asks for the ruleset when it wakes, which is always the latest one.
    /// </summary>
    public sealed class Subscription(RulesetNotifier notifier, long environmentId) : IDisposable
    {
        private readonly Channel<bool> pending = Channel.CreateBounded<bool>(new BoundedChannelOptions(1) { FullMode = BoundedChannelFullMode.DropOldest });

        /// <summary>Completes when a change is pending, and takes it.</summary>
        public Task Next(CancellationToken cancellation) => pending.Reader.ReadAsync(cancellation).AsTask();

        internal void Signal() => pending.Writer.TryWrite(true);

        public void Dispose() => notifier.Remove(this, environmentId);
    }
}
