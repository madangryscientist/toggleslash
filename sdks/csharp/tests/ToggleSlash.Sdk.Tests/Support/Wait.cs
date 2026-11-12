namespace ToggleSlash.Sdk.Tests.Support;

internal static class Wait
{
    /// <summary>
    /// Waits for a condition, checking it every few milliseconds. The deadline is only the bound at which a hang fails the
    /// test; a quick machine and a loaded one both pass as soon as the condition holds.
    /// </summary>
    public static async Task UntilAsync(Func<bool> condition, string what, int timeoutMs = 30_000)
    {
        var deadline = Environment.TickCount64 + timeoutMs;
        while (!condition())
        {
            if (Environment.TickCount64 > deadline)
            {
                throw new TimeoutException("timed out waiting for " + what);
            }

            await Task.Delay(2);
        }
    }

    public static async Task<T> WithinAsync<T>(Task<T> task, string what, int timeoutMs = 30_000)
    {
        var finished = await Task.WhenAny(task, Task.Delay(timeoutMs));
        return finished == task ? await task : throw new TimeoutException(what + " did not finish");
    }

    public static async Task WithinAsync(Task task, string what, int timeoutMs = 30_000)
    {
        var finished = await Task.WhenAny(task, Task.Delay(timeoutMs));
        if (finished != task)
        {
            throw new TimeoutException(what + " did not finish");
        }

        await task;
    }
}
