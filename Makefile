# Native (cgo) builds run in the Debian 11 build container, never on a host.
#
# SPIKE_ROOT is a directory with room for build caches and third-party
# sources: $(SPIKE_ROOT)/cache and $(SPIKE_ROOT)/src. Set it in the
# environment or on the command line; it is not stored in the repository.

IMAGE      ?= automaticum-build:deb11
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

.PHONY: image shell test

image:
	docker build -t $(IMAGE) -f build/Dockerfile.build build

shell:
	$(DOCKER_RUN) -it $(IMAGE) bash

# Pure-Go packages only; the store's cgo tests have their own targets.
test:
	go test ./...
