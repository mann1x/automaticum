# Language layer: C and C++ with GCC 14, the oracle image itself (gcc:14,
# pinned by digest). That image is Debian 13: its compiler needs a newer C
# library than the base's Debian 12, so this layer starts from it and takes
# Node and the runner from the base instead (rebuild the base first).
# Built by `automaticum image build cpp`.
FROM automaticum-lane:base AS lane
FROM gcc@sha256:db19db55a3bd1f836212a76854c4f6c7c4ccb0c9a9426dbf542e3505bc8cdfec

# The base's tools; make, binutils and the C library headers are in the image.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl git libatomic1 less patch procps ripgrep unzip xz-utils \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 1000 node && useradd --uid 1000 --gid node --shell /bin/bash --create-home node
COPY --from=lane /usr/local/bin/node /usr/local/bin/
COPY --from=lane /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=lane /usr/local/include/node /usr/local/include/node
COPY --from=lane /opt/automaticum /opt/automaticum
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && gcc --version | head -1 && g++ --version | head -1 && make --version | head -1 && node --version
WORKDIR /workspace
