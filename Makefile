# Native (cgo) builds run in the Debian 11 build container, never on a host.
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

.PHONY: images lance-c test test-store shell

# The link image (final cgo link, manylinux_2_28) and the cross image (Rust
# archives for other targets, Debian 11).
images:
	docker build -t $(LINK_IMAGE) -f build/Dockerfile.build build
	docker build -t $(CROSS_IMAGE) -f build/Dockerfile.cross build

# Build lance-c's static library from $(SPIKE_ROOT)/src/lance-c.
lance-c:
	$(DOCKER_RUN) -e CARGO_TARGET_DIR=/target/lance-c -w /work/lance-c $(CROSS_IMAGE) \
		cargo +1.99.0 build --release --target x86_64-unknown-linux-gnu

# Pure-Go packages: run on the host.
test:
	go test -race ./internal/store/blob/... ./internal/legacyimport/...

# Packages that link DuckDB and Lance: run in the link image.
test-store:
	$(DOCKER_RUN) -e CGO_CFLAGS=-I$(LANCE_C)/include -e CGO_LDFLAGS=$(LANCE_LIB) $(LINK_IMAGE) \
		go test -tags lance -count=1 ./internal/store/...

shell:
	$(DOCKER_RUN) -it $(LINK_IMAGE) bash
