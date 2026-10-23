# Language layer: Rust 1.83, the oracle image's own toolchain (rust:1.83
# on Debian 12, pinned by digest), with Debian's C linker. Offline: no
# crates beyond the standard library. Built by `automaticum image build rust`.
FROM rust@sha256:540c902e99c384163b688bbd8b5b8520e94e7731b27f7bd0eaa56ae1960627ab AS rs
FROM automaticum-lane:base

# rustc links through cc.
RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc libc6-dev \
 && rm -rf /var/lib/apt/lists/*
COPY --from=rs /usr/local/rustup /usr/local/rustup
COPY --from=rs /usr/local/cargo /usr/local/cargo
ENV RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo PATH=/usr/local/cargo/bin:$PATH CARGO_NET_OFFLINE=true
RUN chmod -R a+rwX /usr/local/cargo && rustc --version && cargo --version
