using ToggleSlash.Server.Data;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Mvc.Testing;
using Microsoft.Data.Sqlite;

/// <summary>A directory under <c>$TMPDIR</c> holding one test's database file, removed afterwards.</summary>
public sealed class TempDatabase : IDisposable
{
    private readonly DirectoryInfo directory = Directory.CreateTempSubdirectory("toggleslash-db-");

    public string Path => System.IO.Path.Combine(directory.FullName, "test.db");

    /// <summary>A connection that bypasses the server, as anything else reading the file would.</summary>
    public SqliteConnection Open()
    {
        var connection = new SqliteConnection(new SqliteConnectionStringBuilder { DataSource = Path, Pooling = false }.ToString());
        connection.Open();
        return connection;
    }

    /// <summary>
    /// The bytes of every file SQLite keeps for this database, the write-ahead log included. A connection of the
    /// scan's own (not pooled, so <c>ClearAllPools</c> in another test cannot close it) stays open until the last file
    /// is read: SQLite checkpoints and deletes the log only when the last connection closes, so the directory holds
    /// still and the row is in the log or in the database file, never in neither.
    /// </summary>
    public async Task<IReadOnlyList<byte[]>> ReadEveryFile()
    {
        using var holder = Open();
        using (var touch = holder.CreateCommand())
        {
            // A statement makes SQLite open the log and its shared memory; an idle connection holds neither.
            touch.CommandText = "SELECT count(*) FROM sqlite_master";
            touch.ExecuteScalar();
        }

        var files = Directory.GetFiles(directory.FullName);
        var contents = new List<byte[]>(files.Length);
        foreach (var file in files)
        {
            using var stream = new FileStream(file, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete);
            using var buffer = new MemoryStream();
            await stream.CopyToAsync(buffer);
            contents.Add(buffer.ToArray());
        }

        return contents;
    }

    public void Dispose()
    {
        SqliteConnection.ClearAllPools();
        directory.Delete(recursive: true);
    }
}

/// <summary>The real server, startup included, on a database file of its own.</summary>
public sealed class ServerOn(string databasePath, Action<IWebHostBuilder>? configure = null) : WebApplicationFactory<Program>
{
    protected override void ConfigureWebHost(IWebHostBuilder builder)
    {
        builder.UseSetting(ServerDatabase.PathSetting, databasePath);
        configure?.Invoke(builder);
    }

    /// <summary>Runs the startup path, which ends once the host is up.</summary>
    public void Start() => _ = Services;
}

/// <summary>A class fixture for tests that need a running server and do not care about its database.</summary>
public sealed class TempDatabaseServer : IDisposable
{
    private readonly TempDatabase database = new();
    private readonly ServerOn server;

    public TempDatabaseServer() => server = new ServerOn(database.Path);

    public HttpClient CreateClient() => server.CreateClient();

    public void Dispose()
    {
        server.Dispose();
        database.Dispose();
    }
}
