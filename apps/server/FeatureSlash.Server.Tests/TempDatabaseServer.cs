using FeatureSlash.Server.Data;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Mvc.Testing;
using Microsoft.Data.Sqlite;

/// <summary>A directory under <c>$TMPDIR</c> holding one test's database file, removed afterwards.</summary>
public sealed class TempDatabase : IDisposable
{
    private readonly DirectoryInfo directory = Directory.CreateTempSubdirectory("featureslash-db-");

    public string Path => System.IO.Path.Combine(directory.FullName, "test.db");

    /// <summary>A connection that bypasses the server, as anything else reading the file would.</summary>
    public SqliteConnection Open()
    {
        var connection = new SqliteConnection(new SqliteConnectionStringBuilder { DataSource = Path, Pooling = false }.ToString());
        connection.Open();
        return connection;
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
