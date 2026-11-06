namespace ToggleSlash.Server.Targeting;

/// <summary>
/// Held while a config, a segment or a flag's metadata is written (a flag edit holds it only so its audit event's before and after are true). A config save checks other rows (the flags and segments
/// it names, the prerequisite graph) and a segment delete checks every config, and SQLite would let two
/// of those both pass and then both write, so they take turns. One self-hosted process owns one file
/// (docs/architecture.md), so a lock in the process is enough. Reads never take it.
/// </summary>
public sealed class TargetingLock
{
    private readonly SemaphoreSlim gate = new(1, 1);

    internal async Task<IDisposable> Enter(CancellationToken cancellation)
    {
        await gate.WaitAsync(cancellation);
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
