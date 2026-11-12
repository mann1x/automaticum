# Language layer: Rust 1.83, the oracle image's own toolchain (rust:1.83
# on Debian 12, pinned by digest), with Debian's C linker. Offline: no
# crates beyond the standard library. Built by `automaticum image build rust`.
FROM rust@sha256:540c902e99c384163b688bbd8b5b8520e94e7731b27f7bd0eaa56ae1960627ab AS pinned
# The language server of an arm with [agent.lsp] (D149): the toolchain's
# own rust-analyzer component, and the standard library's source it reads.
FROM pinned AS rs
RUN rustup component add rust-analyzer rust-src
FROM automaticum-lane:base

# rustc links through cc.
RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc libc6-dev \
 && rm -rf /var/lib/apt/lists/*
COPY --from=rs /usr/local/rustup /usr/local/rustup
COPY --from=rs /usr/local/cargo /usr/local/cargo
ENV RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo PATH=/usr/local/cargo/bin:$PATH CARGO_NET_OFFLINE=true
# The server goes under the runner's prefix (D113). rustup's own
# rust-analyzer proxy stays in cargo/bin, as in every rustup install.
RUN chmod -R a+rwX /usr/local/cargo && rustc --version && cargo --version \
 && mkdir -p /opt/automaticum/lsp/bin \
 && ln -s "$(rustup which rust-analyzer)" /opt/automaticum/lsp/bin/rust-analyzer \
 && /opt/automaticum/lsp/bin/rust-analyzer --version
