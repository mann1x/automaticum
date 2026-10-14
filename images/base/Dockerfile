# The lane's base image: what every run's container starts from.
# Built by `automaticum image build base` from the repository root.
#
# It holds no SDK. The SDK build a run's arm names is provisioned apart
# (`automaticum sdk add`) and mounted read-only at the runner's node_modules
# when the run starts, so one image serves every build.
FROM node@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392

# libatomic1: single executables built from Node need it (task oracles).
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl git libatomic1 less patch procps ripgrep unzip xz-utils \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/automaticum/runner
COPY runner/package.json runner/runner.mjs runner/session.mjs runner/user.mjs runner/wire.mjs ./
# Where the SDK bundle is mounted.
RUN mkdir node_modules

WORKDIR /workspace
