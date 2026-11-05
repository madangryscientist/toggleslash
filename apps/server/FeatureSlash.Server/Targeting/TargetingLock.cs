namespace FeatureSlash.Server.Targeting;

/// <summary>
/// Held while a config or a segment is written. A config save checks other rows (the flags and segments
/// it names, the prerequisite graph) and a segment delete checks every config, and SQLite would let two
/// of those both pass and then both write, so they take turns. One self-hosted process owns one file
/// (docs/architecture.md), so a lock in the process is enough. Reads never take it.
/// </summary>
public sealed class TargetingLock
{
    private readonly SemaphoreSlim gate = new(1, 1);

    internal async Task<IDisposable> Enter()
    {
        await gate.WaitAsync();
        return new Releaser(gate);
    }

    private sealed class Releaser(SemaphoreSlim gate) : IDisposable
    {
        private int released;

        public void Dispose()
        {
            if (Interlocked.Exchange(ref released, 1) == 0)
            {
                gate.Release();
            }
        }
    }
}
