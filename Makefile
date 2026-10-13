# Native (cgo) builds run in the build containers, never on a host.
#
# SPIKE_ROOT is a directory with room for build caches and third-party
# sources: $(SPIKE_ROOT)/cache and $(SPIKE_ROOT)/src. Set it in the
# environment or on the command line; it is not stored in the repository.

LINK_IMAGE  ?= automaticum-build:ml228-amd64
CROSS_IMAGE ?= automaticum-build:deb11
LANCE_C     ?= /work/lance-c
LANCE_LIB   ?= /target/lance-c/x86_64-unknown-linux-gnu/release/liblance_c.a
SPIKE_ROOT ?= $(error set SPIKE_ROOT to a directory for caches and sources)

DOCKER_RUN = docker run --rm \
	-v $(SPIKE_ROOT)/src:/work \
	-v $(CURDIR):/src \
	-v $(SPIKE_ROOT)/cache/gomod:/go/pkg/mod \
	-v $(SPIKE_ROOT)/cache/gobuild:/root/.cache/go-build \
	-v $(SPIKE_ROOT)/cache/cargo:/opt/cargo/registry \
	-v $(SPIKE_ROOT)/cache/cargo-git:/opt/cargo/git \
	-v $(SPIKE_ROOT)/cache/target:/target \
	-e GOMODCACHE=/go/pkg/mod -e GOFLAGS=-buildvcs=false

.PHONY: images lance-c build bench test test-store shell

# The link image (final cgo link, manylinux_2_28) and the cross image (Rust
# archives for other targets, Debian 11).
images:
	docker build -t $(LINK_IMAGE) -f build/Dockerfile.build build
	docker build -t $(CROSS_IMAGE) -f build/Dockerfile.cross build

# Build lance-c's static library from $(SPIKE_ROOT)/src/lance-c.
lance-c:
	$(DOCKER_RUN) -e CARGO_TARGET_DIR=/target/lance-c -w /work/lance-c $(CROSS_IMAGE) \
		cargo +1.99.0 build --release --target x86_64-unknown-linux-gnu

# The command line (DuckDB and the blob store; no Lance yet) into bin/.
build:
	$(DOCKER_RUN) -w /src $(LINK_IMAGE) go build -o bin/automaticum ./cmd/automaticum
	$(DOCKER_RUN) -w /src $(LINK_IMAGE) go build -o bin/automaticumd ./cmd/automaticumd

# The spike's measurement program into bin/.
bench:
	$(DOCKER_RUN) -w /src -e CGO_CFLAGS=-I$(LANCE_C)/include -e CGO_LDFLAGS=$(LANCE_LIB) $(LINK_IMAGE) \
		go build -tags lance -o bin/bench ./spike/001-store/bench

# Pure-Go packages: run on the host.
test:
	go test -race ./internal/store/blob/... ./internal/legacyimport/... ./internal/config/... ./internal/stubmodel/... ./internal/run/... ./internal/sdk/... ./internal/human/... ./internal/detect/... ./internal/gate/... ./internal/ingest/... ./internal/daemon/...
	go test -short ./internal/task/... ./internal/docker/...
	CGO_ENABLED=0 go test ./cmd/automaticum/...

# Packages that link DuckDB and Lance: run in the link image.
test-store:
	$(DOCKER_RUN) -e CGO_CFLAGS=-I$(LANCE_C)/include -e CGO_LDFLAGS=$(LANCE_LIB) $(LINK_IMAGE) \
		go test -tags lance -count=1 ./internal/store/...

shell:
	$(DOCKER_RUN) -it $(LINK_IMAGE) bash
