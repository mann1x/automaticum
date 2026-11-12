# Language layer: C and C++ with GCC 14, the oracle image itself (gcc:14,
# pinned by digest). That image is Debian 13: its compiler needs a newer C
# library than the base's Debian 12, so this layer starts from it and takes
# Node and the runner from the base instead (rebuild the base first).
# Built by `automaticum image build cpp`.
FROM automaticum-lane:base AS lane
FROM gcc@sha256:db19db55a3bd1f836212a76854c4f6c7c4ccb0c9a9426dbf542e3505bc8cdfec

# The base's tools; make, binutils and the C library headers are in the image.
# clangd is the language server of an arm with [agent.lsp] (D149), Debian's;
# the runner starts it from its own prefix, on no PATH (D113). Debian puts a
# clangd link in /usr/bin too. The second list is Chrome's libraries for an
# arm with [agent.browser] (D150), the base's list in Debian 13's names; the
# browser itself comes with /opt/automaticum from the base.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl git libatomic1 less patch procps ripgrep unzip xz-utils \
 && apt-get install -y --no-install-recommends clangd \
 && apt-get install -y --no-install-recommends \
      fonts-liberation libasound2t64 libatk-bridge2.0-0t64 libatk1.0-0t64 libatspi2.0-0t64 libcairo2 libcups2t64 libcurl4t64 \
      libdbus-1-3 libdrm2 libexpat1 libgbm1 libglib2.0-0t64 libnspr4 libnss3 libpango-1.0-0 libudev1 libvulkan1 \
      libx11-6 libxcb1 libxcomposite1 libxdamage1 libxext6 libxfixes3 libxkbcommon0 libxrandr2 \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 1000 node && useradd --uid 1000 --gid node --shell /bin/bash --create-home node
COPY --from=lane /usr/local/bin/node /usr/local/bin/
COPY --from=lane /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=lane /usr/local/include/node /usr/local/include/node
COPY --from=lane /opt/automaticum /opt/automaticum
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && ln -s "$(readlink -f /usr/bin/clangd)" /opt/automaticum/lsp/bin/clangd \
 && /opt/automaticum/lsp/bin/clangd --version | head -1 \
 && /opt/automaticum/browser/chrome-linux64/chrome --version \
 && gcc --version | head -1 && g++ --version | head -1 && make --version | head -1 && node --version
WORKDIR /workspace
