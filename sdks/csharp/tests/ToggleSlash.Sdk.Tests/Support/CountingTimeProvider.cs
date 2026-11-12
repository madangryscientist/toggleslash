using Microsoft.Extensions.Time.Testing;

namespace ToggleSlash.Sdk.Tests.Support;

/// <summary>
/// A <see cref="FakeTimeProvider"/> that says how many timers are armed. The fake one does not, and stepping it before a
/// loop has armed its wait loses the step, so a test advances only after <see cref="WaitForArmedAsync"/>.
/// </summary>
internal sealed class CountingTimeProvider : FakeTimeProvider
{
    private int _armed;

    public CountingTimeProvider(DateTimeOffset start)
        : base(start)
    {
    }

    public int Armed => Volatile.Read(ref _armed);

    public override ITimer CreateTimer(TimerCallback callback, object? state, TimeSpan dueTime, TimeSpan period)
    {
        var inner = base.CreateTimer(callback, state, dueTime, period);
        return new Counted(this, inner, dueTime);
    }

    public Task WaitForArmedAsync(int count) =>
        Wait.UntilAsync(() => Armed == count, $"{count} armed timers (have {Armed})");

    private sealed class Counted : ITimer
    {
        private readonly CountingTimeProvider _owner;
        private readonly ITimer _inner;
        private bool _armed;
        private bool _disposed;

        public Counted(CountingTimeProvider owner, ITimer inner, TimeSpan due)
        {
            _owner = owner;
            _inner = inner;
            Mark(due);
        }

        public bool Change(TimeSpan dueTime, TimeSpan period)
        {
            Mark(dueTime);
            return _inner.Change(dueTime, period);
        }

        public void Dispose()
        {
            lock (this)
            {
                if (_disposed)
                {
                    return;
                }

                _disposed = true;
                if (_armed)
                {
                    _armed = false;
                    Interlocked.Decrement(ref _owner._armed);
                }
            }

            _inner.Dispose();
        }

        public ValueTask DisposeAsync()
        {
            Dispose();
            return ValueTask.CompletedTask;
        }

        private void Mark(TimeSpan due)
        {
            lock (this)
            {
                var armed = due != Timeout.InfiniteTimeSpan && !_disposed;
                if (armed && !_armed)
                {
                    Interlocked.Increment(ref _owner._armed);
                }
                else if (!armed && _armed)
                {
                    Interlocked.Decrement(ref _owner._armed);
                }

                _armed = armed;
            }
        }
    }
}
