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

# A static ripgrep for the lane layers on a task's own image (066): they
# put it under the runner's prefix, where only the SDK finds it (D113). The
# base's own agent has the package above.
RUN case "$(dpkg --print-architecture)" in \
      amd64) rg=ripgrep-14.1.1-x86_64-unknown-linux-musl sum=4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e ;; \
      *) echo "no pinned static ripgrep for $(dpkg --print-architecture)" >&2; exit 1 ;; \
    esac \
 && curl -fsSL -o /tmp/rg.tgz "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/$rg.tar.gz" \
 && echo "$sum  /tmp/rg.tgz" | sha256sum -c - \
 && mkdir -p /opt/automaticum/tools \
 && tar -xzf /tmp/rg.tgz -C /opt/automaticum/tools --strip-components=1 "$rg/rg" \
 && rm /tmp/rg.tgz && /opt/automaticum/tools/rg --version

WORKDIR /opt/automaticum/runner
COPY runner/package.json runner/runner.mjs runner/guards.mjs runner/plan.mjs runner/session.mjs runner/tools.mjs runner/user.mjs runner/wire.mjs ./
# Where the SDK bundle is mounted, and the runner's own prefix (D113): it
# starts as /opt/automaticum/bin/node, here Node of the image itself.
RUN mkdir node_modules \
 && mkdir -p /opt/automaticum/bin && ln -s /usr/local/bin/node /opt/automaticum/bin/node

WORKDIR /workspace
