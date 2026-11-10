using System.Diagnostics;

namespace ToggleSlash.Sdk.Tests;

public sealed class ProcessTests
{
    [Fact]
    public async Task A_client_that_is_never_closed_does_not_keep_the_process_from_exiting()
    {
        // .../artifacts/bin/ToggleSlash.Sdk.Tests/<config>/ next to .../artifacts/bin/NeverClosed/<config>/
        var here = new DirectoryInfo(AppContext.BaseDirectory.TrimEnd(Path.DirectorySeparatorChar));
        var program = Path.Combine(here.Parent!.Parent!.FullName, "NeverClosed", here.Name, "NeverClosed.dll");
        Assert.True(File.Exists(program), "not built: " + program);
        var start = new ProcessStartInfo(Environment.ProcessPath!, [program]) { RedirectStandardOutput = true, UseShellExecute = false };
        using var process = Process.Start(start)!;
        using var limit = new CancellationTokenSource(TimeSpan.FromSeconds(30));
        try
        {
            await process.WaitForExitAsync(limit.Token);
        }
        catch (OperationCanceledException)
        {
            process.Kill(entireProcessTree: true);
            Assert.Fail("the process did not exit: the client kept it alive");
        }
        Assert.Equal(0, process.ExitCode);
        Assert.Equal("True", (await process.StandardOutput.ReadToEndAsync()).Trim());
    }
}
