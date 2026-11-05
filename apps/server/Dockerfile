# The FeatureSlash server, for self-hosting (docs/self-hosting.md).
#
# Build from the repository root, which is the context (the server references the evaluator project
# and the SDK is pinned by global.json):
#
#   docker build -f apps/server/Dockerfile -t featureslash-server .
#
# Both base images are pinned by the digest of their multi-arch index, read from
# mcr.microsoft.com; the tag in front is for people. To move to a newer patch, look up the new
# index digest (the Docker-Content-Digest header of GET /v2/dotnet/<sdk|aspnet>/manifests/<tag>
# with an index Accept header) and change both lines. The SDK matches global.json (10.0.402).

FROM mcr.microsoft.com/dotnet/sdk:10.0.402@sha256:747cbade5bfd918a175ac32f8f1d931edec2c7d0d5397a3de72eaef1ac1f66d2 AS build
WORKDIR /src

# Restore first, from the project files alone, so a source-only change reuses this layer.
COPY global.json ./
COPY apps/server/Directory.Build.props apps/server/Directory.Build.rsp apps/server/
COPY apps/server/FeatureSlash.Evaluation/FeatureSlash.Evaluation.csproj apps/server/FeatureSlash.Evaluation/
COPY apps/server/FeatureSlash.Server/FeatureSlash.Server.csproj apps/server/FeatureSlash.Server/
RUN dotnet restore apps/server/FeatureSlash.Server/FeatureSlash.Server.csproj

COPY apps/server/FeatureSlash.Evaluation apps/server/FeatureSlash.Evaluation
COPY apps/server/FeatureSlash.Server apps/server/FeatureSlash.Server
RUN dotnet publish apps/server/FeatureSlash.Server/FeatureSlash.Server.csproj \
    --configuration Release --no-restore --output /out /p:UseAppHost=false

FROM mcr.microsoft.com/dotnet/aspnet:10.0.14@sha256:de0ee358cdd672f7017a003bed21e9ac9ed48a27c922a59b18175b6e82fa7fee AS runtime
WORKDIR /app
COPY --from=build /out ./

# SQLite lives in /data, a volume. The image's own `app` user (APP_UID, 1654) owns it, so a named
# volume starts out writable by the non-root user the server runs as.
RUN mkdir /data && chown "$APP_UID" /data
VOLUME /data
USER $APP_UID

# Port 8080 is the base image's default (ASPNETCORE_HTTP_PORTS). The admin token is never baked in:
# FEATURESLASH_ADMIN_TOKEN is read at run time (see compose.yaml).
ENV FEATURESLASH_DB_PATH=/data/featureslash.db
EXPOSE 8080

# The image has no curl; bash's /dev/tcp is enough to ask /health for a 200.
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD bash -c 'exec 3<>/dev/tcp/127.0.0.1/8080 && printf "GET /health HTTP/1.0\r\nHost: localhost\r\n\r\n" >&3 && head -n1 <&3 | grep -q " 200 "'

ENTRYPOINT ["dotnet", "FeatureSlash.Server.dll"]
