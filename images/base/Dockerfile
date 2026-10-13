# The lane's base image: what every run's container starts from.
# Built by `automaticum image build base` from the repository root.
#
# The SDK is installed from the registry by the runner's lockfile, and the
# release named in the label is checked against what was installed: an image
# whose label and content disagree does not build.
FROM node@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392

ARG SDK_RELEASE

# libatomic1: single executables built from Node need it (task oracles).
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl git libatomic1 less patch procps ripgrep unzip xz-utils \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/automaticum/runner
COPY runner/package.json runner/package-lock.json ./
# Install scripts of dependencies are not run.
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund \
 && npm cache clean --force \
 && test -n "$SDK_RELEASE" \
 && test "$(node -p "require('./node_modules/@cerebriline/sdk/package.json').version")" = "$SDK_RELEASE"
COPY runner/runner.mjs runner/session.mjs runner/wire.mjs ./

LABEL automaticum.sdk.release="$SDK_RELEASE"
WORKDIR /workspace
