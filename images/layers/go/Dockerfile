# Language layer: Go 1.23, the oracle image's own toolchain (golang:1.23 on
# Debian 12, pinned by digest). Offline: no module proxy, no toolchain
# download. Built by `automaticum image build go`.
FROM golang@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db AS go
FROM automaticum-lane:base

COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod CGO_ENABLED=0
RUN go version
