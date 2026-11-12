# Language layer: C# with the .NET 8 SDK, the oracle image's own SDK
# (mcr.microsoft.com/dotnet/sdk:8.0, pinned by digest). Offline: no NuGet
# packages beyond the SDK's; no build server left running after a build.
# Built by `automaticum image build dotnet`.
FROM mcr.microsoft.com/dotnet/sdk@sha256:738f72aff955d5c5690e06b6fc8ecd1b90ead564099a257ac528ff720b8da85f AS net
FROM automaticum-lane:base

RUN apt-get update \
 && apt-get install -y --no-install-recommends libicu72 libssl3 libstdc++6 zlib1g \
 && rm -rf /var/lib/apt/lists/*
COPY --from=net /usr/share/dotnet /usr/share/dotnet
RUN ln -s /usr/share/dotnet/dotnet /usr/local/bin/dotnet
ENV DOTNET_ROOT=/usr/share/dotnet DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 \
    DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1 DOTNET_GENERATE_ASPNET_CERTIFICATE=false \
    DOTNET_CLI_USE_MSBUILD_SERVER=0 MSBUILDDISABLENODEREUSE=1
# The language server of an arm with [agent.lsp] (D149): csharp-ls 0.16.0,
# its last release on .NET 8 (0.17.0 on need .NET 9), installed while the
# build may fetch, under the runner's prefix, on no PATH (D113).
RUN dotnet tool install csharp-ls --version 0.16.0 --tool-path /opt/automaticum/lsp/csharp-ls \
 && ln -s ../csharp-ls/csharp-ls /opt/automaticum/lsp/bin/csharp-ls \
 && rm -rf /root/.nuget /root/.local/share/NuGet /tmp/NuGetScratch* \
 && dotnet --info | head -3 && /opt/automaticum/lsp/bin/csharp-ls --version
