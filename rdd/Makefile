# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors
# SPDX-FileCopyrightText: The KCP Authors

# Delete the target of a recipe that fails, so make cannot take a truncated
# file for a good one. GNU make applies this to every target in the file;
# listing prerequisites does not narrow it.
.DELETE_ON_ERROR:

EXE := $(if $(shell sh -c 'command -v winver.exe'),.exe,)
KUBE_VERSION := $(shell go$(EXE) list -m -f '{{.Version}}' 'k8s.io/kubernetes')
KUBE_MAJOR_VERSION := $(shell echo $(KUBE_VERSION) | sed 's/v\([0-9]*\).*/\1/')
KUBE_MINOR_VERSION := $(shell echo $(KUBE_VERSION) | sed "s/v[0-9]*\.\([0-9]*\).*/\1/")
GIT_COMMIT := $(shell git$(EXE) rev-parse --short HEAD || echo 'local')
GIT_DIRTY := $(shell git$(EXE) diff --quiet && echo 'clean' || echo 'dirty')
GIT_VERSION := $(KUBE_VERSION)+rdd-$(shell git$(EXE) describe --tags --match='v*' --abbrev=14 "$(GIT_COMMIT)^{commit}" 2>/dev/null || echo v0.0.0-$(GIT_COMMIT))
RDD_VERSION := $(shell git$(EXE) describe --match 'v[0-9]*' --dirty='.m' --always --tags)
RDD_VERSION_TRIMMED := $(RDD_VERSION:v%=%)
LIMA_VERSION := $(shell go$(EXE) list -m -f '{{.Version}}' 'github.com/lima-vm/lima/v2')
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
PACKAGE := github.com/rancher-sandbox/rancher-desktop-daemon
LDFLAGS := \
	-X k8s.io/client-go/pkg/version.gitCommit=${GIT_COMMIT} \
	-X k8s.io/client-go/pkg/version.gitTreeState=${GIT_DIRTY} \
	-X k8s.io/client-go/pkg/version.gitVersion=${GIT_VERSION} \
	-X k8s.io/client-go/pkg/version.gitMajor=${KUBE_MAJOR_VERSION} \
	-X k8s.io/client-go/pkg/version.gitMinor=${KUBE_MINOR_VERSION} \
	-X k8s.io/client-go/pkg/version.buildDate=${BUILD_DATE} \
	\
	-X k8s.io/component-base/version.gitCommit=${GIT_COMMIT} \
	-X k8s.io/component-base/version.gitTreeState=${GIT_DIRTY} \
	-X k8s.io/component-base/version.gitVersion=${GIT_VERSION} \
	-X k8s.io/component-base/version.gitMajor=${KUBE_MAJOR_VERSION} \
	-X k8s.io/component-base/version.gitMinor=${KUBE_MINOR_VERSION} \
	-X k8s.io/component-base/version.buildDate=${BUILD_DATE} \
	\
	-X $(PACKAGE)/pkg/version.Version=$(RDD_VERSION) \
	-X $(PACKAGE)/pkg/version.GitCommit=${GIT_COMMIT} \
	-X $(PACKAGE)/pkg/version.BuildDate=${BUILD_DATE} \
	-X github.com/lima-vm/lima/v2/pkg/version.Version=$(LIMA_VERSION) \
	-s -w

TAGS := sqlite_omit_load_extension

# DISTRO_IMAGE is the guest distro image bin/rdd and bin/lima-controller embed
# under the with_distro tag. WSL2 imports a rootfs tarball, and Lima's other
# drivers boot a raw disk image.
#
# The build target is a GOOS or GOARCH set on the make command line, which GNU
# make 3.81 hides from $(shell), or else what `go env` reports. The recipes
# that build binaries pass the target to go explicitly, through WSLENV too, so
# bin/rdd and bin/lima-controller embed the image and guest agent staged for
# the target they are compiled for.
TARGET_OS := $(if $(filter command line,$(origin GOOS)),$(GOOS),$(shell go$(EXE) env GOOS))
TARGET_ARCH := $(if $(filter command line,$(origin GOARCH)),$(GOARCH),$(shell go$(EXE) env GOARCH))
HOST_OS := $(shell go$(EXE) env GOHOSTOS)
HOST_ARCH := $(shell go$(EXE) env GOHOSTARCH)
DISTRO_VARIANT := $(if $(filter windows,$(TARGET_OS)),tar,raw)
DISTRO_IMAGE := pkg/embedded/distro.$(DISTRO_VARIANT).xz

# The overlay adds the binaries the distro stopped shipping once Rancher
# Desktop took them over. download-guest-deps stages the pristine distro and
# those downloads into OVERLAY_BUILD, and overlay/manifest.yaml says where each
# one goes in the guest.
OVERLAY_BUILD := overlay/build
PRISTINE_DISTRO := $(OVERLAY_BUILD)/distro.$(DISTRO_VARIANT).xz
WORKING_DISTRO := $(OVERLAY_BUILD)/distro.$(DISTRO_VARIANT)
MKCERT := $(OVERLAY_BUILD)/mkcert
NERDCTL_TARBALL := $(OVERLAY_BUILD)/nerdctl-full.tar.gz
NERDCTL_DIR := $(OVERLAY_BUILD)/nerdctl
NERDCTL_BINARIES := $(addprefix $(NERDCTL_DIR)/bin/,buildctl buildkitd nerdctl)
OVERLAY_SOURCES := overlay/manifest.yaml overlay/nerdctl $(MKCERT) $(NERDCTL_BINARIES)
OVERLAY_TOOL_SOURCES := $(filter-out %_test.go, \
	$(wildcard cmd/distro-overlay/*.go pkg/overlay/*.go pkg/sparse/*.go))

# One timestamp for every entry the overlay writes, so the same commit always
# produces the same image.
OVERLAY_MTIME := $(shell git$(EXE) log -1 --format=%ct 2>/dev/null || echo 0)

# Compressing the overlaid image dominates this build, and the image size
# matters only for a build we ship, so the default trades bytes for time. The
# packaging workflow overrides it.
XZ_OPTIONS ?= -1 --threads=0

# The guest agent's file name carries the target architecture, and
# pkg/guestagent embeds the one matching GOARCH, so a build for another
# architecture builds its own agent instead of reusing a stale one.
GUESTAGENT_BINARY := pkg/guestagent/lima-guestagent.$(TARGET_ARCH)
GUESTAGENT_GZ := $(GUESTAGENT_BINARY).gz

default: build-rdd
.PHONY: default

ldflags:
	@echo $(LDFLAGS)

build: build-rdd build-all-controllers
.PHONY: build

# Code signing for macOS - required for Virtualization.framework
# On macOS, sign binaries with virtualization entitlement using ad-hoc signature
# On other platforms, this is a no-op
ifeq ($(shell uname -s),Darwin)
define ATTACH_ENTITLEMENTS
codesign --force --verbose --entitlements macos-entitlements.plist --sign - $(1)
endef
else
define ATTACH_ENTITLEMENTS
@true
endef
endif

GOLANG_SOURCES := $(shell find . -name '*.go') go.mod go.sum
EMBEDDED_SOURCES := $(shell find ./cmd ./pkg -name '*.yaml' -o -name '*.json')

# Build the Lima guest agent from our Go dependency and compress it for embedding.
$(GUESTAGENT_BINARY): go.mod go.sum
	WSLENV=${WSLENV}:CGO_ENABLED:GOOS:GOARCH \
	CGO_ENABLED=0 GOOS=linux GOARCH=$(TARGET_ARCH) \
	go$(EXE) build -ldflags="-s -w" -o $@ \
		github.com/lima-vm/lima/v2/cmd/lima-guestagent

$(GUESTAGENT_GZ): $(GUESTAGENT_BINARY)
	gzip --to-stdout $< > $@

.INTERMEDIATE: $(GUESTAGENT_BINARY)

# The downloader runs on every build, because the staged file names have no
# architecture in them and only a rerun notices a build for a different one. It
# leaves up-to-date files untouched, so make does not redo the overlay.
# Naming the host makes `go run` build the tool for this machine; an empty
# GOOS or GOARCH would fall back to a target saved with `go env -w`.
$(PRISTINE_DISTRO): FORCE
	WSLENV=${WSLENV}:GOOS:GOARCH GOOS=$(HOST_OS) GOARCH=$(HOST_ARCH) \
	go$(EXE) run ./cmd/download-guest-deps --os $(TARGET_OS) --arch $(TARGET_ARCH) \
		--dest $(OVERLAY_BUILD)

# That same run stages these, so they have no recipe of their own.
$(MKCERT) $(NERDCTL_TARBALL): $(PRISTINE_DISTRO) ;

# nerdctl-full also holds containerd, runc and the CNI plugins, which the
# distro installs from its own repositories. tar restores the archive's own
# timestamps, which predate the download, so without the touch make unpacks
# the tarball again on every build.
$(NERDCTL_DIR)/bin/nerdctl: $(NERDCTL_TARBALL)
	mkdir -p $(NERDCTL_DIR)
	tar --extract --gzip --file=$< --directory=$(NERDCTL_DIR) \
		bin/buildctl bin/buildkitd bin/nerdctl
	touch $(NERDCTL_BINARIES)

$(NERDCTL_DIR)/bin/buildctl $(NERDCTL_DIR)/bin/buildkitd: $(NERDCTL_DIR)/bin/nerdctl ;

# distro-overlay works on the uncompressed distro, so the build unpacks the
# pristine artifact and overlays that copy in place. The copy is over a
# gigabyte, and make deletes an intermediate after a failed or interrupted
# build as well as a finished one.
$(WORKING_DISTRO): $(PRISTINE_DISTRO) $(OVERLAY_SOURCES) $(OVERLAY_TOOL_SOURCES)
	@command -v xz >/dev/null || { \
		echo 'xz is missing; install it to build the guest distro image.' >&2; \
		exit 1; }
	xz --decompress --stdout $(PRISTINE_DISTRO) > $@
	WSLENV=${WSLENV}:GOOS:GOARCH GOOS=$(HOST_OS) GOARCH=$(HOST_ARCH) \
	go$(EXE) run ./cmd/distro-overlay --manifest overlay/manifest.yaml --source overlay \
		--mtime $(OVERLAY_MTIME) --in-place $@

.INTERMEDIATE: $(WORKING_DISTRO)

$(DISTRO_IMAGE): $(WORKING_DISTRO)
	xz $(XZ_OPTIONS) --stdout $< > $@
	ls -lh $@

FORCE:
.PHONY: FORCE

# Only bin/rdd and bin/lima-controller embed the distro. Tests, lint and the
# other standalone controllers build without with_distro and need no image.
bin/rdd$(EXE): $(GOLANG_SOURCES) $(EMBEDDED_SOURCES) $(GUESTAGENT_GZ) $(DISTRO_IMAGE)
	WSLENV=${WSLENV}:CGO_CFLAGS:CGO_ENABLED:GOOS:GOARCH \
	CGO_CFLAGS="-DSQLITE_ENABLE_DBSTAT_VTAB=1 -DSQLITE_USE_ALLOCA=1" CGO_ENABLED=1 \
	GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) \
	go$(EXE) build -tags="$(TAGS) with_distro" -gcflags="all=${GCFLAGS}" -ldflags="$(LDFLAGS)" -o $@ ./cmd/rdd
	$(call ATTACH_ENTITLEMENTS,$@)
	ls -lh $@
build-rdd: bin/rdd$(EXE)
.PHONY: build-rdd

# API Group Controller management - Auto-discovery of API groups
CONTROLLERS := $(patsubst cmd/%-controller,%,$(wildcard cmd/*-controller))

# Per-controller CGO settings (default is 0)
# lima-controller requires CGO on macOS for the vz driver (Virtualization.framework)
ifeq ($(shell uname -s),Darwin)
CGO_ENABLED_lima := 1
endif

# The lima controller embeds the guest agent, and the distro it extracts for
# the App's VM.
TAGS_lima := with_distro
bin/lima-controller$(EXE): $(DISTRO_IMAGE) $(GUESTAGENT_GZ)

# Generate build targets for API group controllers
define CONTROLLER_TARGETS
bin/$(1)-controller$$(EXE): $$(GOLANG_SOURCES) $$(EMBEDDED_SOURCES)
	WSLENV=${WSLENV}:CGO_ENABLED:GOOS:GOARCH CGO_ENABLED=$$(or $$(CGO_ENABLED_$(1)),0) \
	GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) \
	go$$(EXE) build -tags="$$(strip $(TAGS) $$(TAGS_$(1)))" -gcflags="all=$${GCFLAGS}" -ldflags="$(LDFLAGS)" -o $$@ ./cmd/$(1)-controller
	$$(call ATTACH_ENTITLEMENTS,$$@)
	ls -lh $$@
build-$(1)-controller: bin/$(1)-controller$$(EXE)
.PHONY: build-$(1)-controller

test-$(1)-controllers:
	go$$(EXE) test -v ./pkg/controllers/$(1)/...
.PHONY: test-$(1)-controllers

run-$(1)-controller: bin/$(1)-controller$$(EXE)
	$$<
.PHONY: run-$(1)-controller
endef

# Generate targets for each API group
$(foreach controller,$(CONTROLLERS),$(eval $(call CONTROLLER_TARGETS,$(controller))))

# The mock controller depends on static test data.
bin/mock-controller$(EXE): $(wildcard pkg/controllers/mock/testdata/*.json)

# Meta targets
build-all-controllers: $(addprefix build-,$(addsuffix -controller,$(CONTROLLERS)))
.PHONY: build-all-controllers

test-all-controllers: $(addprefix test-,$(addsuffix -controllers,$(CONTROLLERS)))
.PHONY: test-all-controllers

run-all-controllers: $(addprefix run-,$(addsuffix -controller,$(CONTROLLERS)))
.PHONY: run-all-controllers

# Code generation targets
generate-deepcopy:
	scripts/generate-deepcopy.sh
.PHONY: generate-deepcopy

generate-crds:
	scripts/generate-crds.sh
.PHONY: generate-crds

generate: generate-deepcopy generate-crds
.PHONY: generate

verify-generate: generate
	@if [ -n "$$(git diff --name-only)" ]; then \
		echo "ERROR: Generated files are out of date. Run 'make generate' and commit the result." >&2; \
		git diff --stat; \
		exit 1; \
	fi
.PHONY: verify-generate

run: bin/rdd$(EXE)
	$< service start
.PHONY: run

test: $(GOLANG_SOURCES) $(GUESTAGENT_GZ)
	go$(EXE) test ./...
.PHONY: test

lint-bats:
	$(MAKE) -C bats lint
.PHONY: lint-bats

lint-rdd: $(GUESTAGENT_GZ)
	go$(EXE) tool golangci-lint run
.PHONY: lint-rdd

lint: lint-bats lint-rdd
.PHONY: lint

format:
	go$(EXE) tool golangci-lint fmt
.PHONY: format

LTAG_EXCLUDES := \
	bats/lib \
	pkg/util/nxadmtail \
	$(GUESTAGENT_BINARY) \
	${NULL}
ltag:
	go$(EXE) tool ltag -v -t .ltag -path . --gitignore --excludes="${LTAG_EXCLUDES}"
check-ltag:
	go$(EXE) tool ltag -v -t .ltag -path . --gitignore --excludes="${LTAG_EXCLUDES}" --check
.PHONY: ltag check-ltag

BATS_TARGETS := $(shell MAKEFLAGS= $(MAKE) -C bats --print-data-base --question --no-builtin-variables | awk -F: '$$1 ~ /^bats-/ { print $$1 }')
$(BATS_TARGETS): bin/rdd$(EXE)
	@$(MAKE) -C bats $@
.PHONY: $(BATS_TARGETS)

check: test lint check-ltag
.PHONY: check

clean:
	-rm -r bin
	-rm pkg/guestagent/lima-guestagent.*
	-rm pkg/embedded/distro.*.xz
	-rm -r $(OVERLAY_BUILD)
.PHONY: clean
