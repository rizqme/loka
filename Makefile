.PHONY: all build build-linux build-linux-amd64 build-linux-arm64 build-all proto clean test test-unit test-integration lint fmt help \
       install uninstall install-vm-assets install-cloud-hypervisor e2e-test kernel kernel-update

# Variables
GO := go
GOFLAGS := -trimpath
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-s -w -X github.com/vyprai/loka/pkg/version.Version=$(VERSION) -X github.com/vyprai/loka/pkg/version.Commit=$(COMMIT) -X github.com/vyprai/loka/pkg/version.BuildTime=$(BUILD_TIME)"

# Platform detection
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

# Cloud Hypervisor version (Linux VMM)
CH_VERSION ?= v44.0

# Binaries
BIN_DIR := bin
LOKAD := $(BIN_DIR)/lokad
LOKA_WORKER := $(BIN_DIR)/loka-worker
LOKA_SUPERVISOR := $(BIN_DIR)/loka-supervisor
LOKA_VMAGENT := $(BIN_DIR)/loka-vmagent
LOKA_CLI := $(BIN_DIR)/loka

# Default target
all: build

# Build all binaries for current platform.
# lokavm is a library (pkg/lokavm), linked into lokad directly.
build: $(LOKAD) $(LOKA_WORKER) $(LOKA_SUPERVISOR) $(LOKA_VMAGENT) $(LOKA_CLI)

$(LOKAD):
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $@ ./cmd/lokad
ifeq ($(UNAME_S),Darwin)
	@printf '<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict><key>com.apple.security.virtualization</key><true/></dict></plist>' > /tmp/lokad.entitlements
	@codesign --entitlements /tmp/lokad.entitlements --force -s - $@ 2>/dev/null || true
	@rm -f /tmp/lokad.entitlements
endif

$(LOKA_WORKER):
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $@ ./cmd/loka-worker

$(LOKA_SUPERVISOR):
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $@ ./cmd/loka-supervisor

$(LOKA_VMAGENT):
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $@ ./cmd/loka-vmagent

$(LOKA_CLI):
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $@ ./cmd/loka

# Build for Linux amd64 (cross-compile)
build-linux-amd64:
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-amd64/lokad ./cmd/lokad
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-amd64/loka-worker ./cmd/loka-worker
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-amd64/loka-supervisor ./cmd/loka-supervisor
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-amd64/loka-vmagent ./cmd/loka-vmagent
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-amd64/loka ./cmd/loka

# Build for Linux arm64 (cross-compile)
build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-arm64/lokad ./cmd/lokad
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-arm64/loka-worker ./cmd/loka-worker
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-arm64/loka-supervisor ./cmd/loka-supervisor
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-arm64/loka-vmagent ./cmd/loka-vmagent
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/linux-arm64/loka ./cmd/loka

# Build for all Linux architectures
build-linux: build-linux-amd64 build-linux-arm64

# Build all platforms
build-all: build build-linux

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	protoc --proto_path=api/proto \
		--go_out=api/lokav1 --go_opt=paths=source_relative \
		--go-grpc_out=api/lokav1 --go-grpc_opt=paths=source_relative \
		types.proto control.proto worker.proto
	protoc --proto_path=api/proto \
		--go_out=api/supervisorv1 --go_opt=paths=source_relative \
		--go-grpc_out=api/supervisorv1 --go-grpc_opt=paths=source_relative \
		supervisor.proto

# Run all tests
test: test-unit

# Unit tests (macOS-safe, no KVM required)
test-unit:
	$(GO) test -v -race -count=1 -tags=unit ./...

# Integration tests (Linux + KVM required)
test-integration:
	$(GO) test -v -race -count=1 -tags=integration ./...

# Install Cloud Hypervisor (Linux VMM backend)
install-cloud-hypervisor:
ifeq ($(UNAME_S),Linux)
	@echo "==> Installing Cloud Hypervisor $(CH_VERSION)"
	@CH_ARCH=$$(uname -m); \
	TMP=$$(mktemp -d); \
	CH_URL="https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/$(CH_VERSION)/cloud-hypervisor-static-$$CH_ARCH"; \
	echo "  Downloading $$CH_URL ..."; \
	curl -fsSL "$$CH_URL" -o "$$TMP/cloud-hypervisor" && \
	chmod +x "$$TMP/cloud-hypervisor" && \
	sudo install -m 755 "$$TMP/cloud-hypervisor" /usr/local/bin/cloud-hypervisor && \
	echo "  cloud-hypervisor → /usr/local/bin/cloud-hypervisor" && \
	rm -rf "$$TMP" || \
	(echo "  Failed to download. Install manually from https://github.com/cloud-hypervisor/cloud-hypervisor/releases"; rm -rf "$$TMP"; exit 1)
else
	@echo "  Cloud Hypervisor is Linux-only (macOS uses Apple Virtualization Framework)"
endif

# Install VM assets (kernel + initramfs) to ~/.loka/vm/
install-vm-assets:
	@mkdir -p $(HOME)/.loka/vm
	@if [ -f build/vmlinux-lokavm ]; then \
		cp build/vmlinux-lokavm $(HOME)/.loka/vm/vmlinux-lokavm; \
		echo "  kernel → ~/.loka/vm/vmlinux-lokavm"; \
	else \
		echo "  ! build/vmlinux-lokavm not found — run 'make kernel' first"; \
	fi
	@if [ -f build/initramfs.cpio.gz ]; then \
		cp build/initramfs.cpio.gz $(HOME)/.loka/vm/initramfs.cpio.gz; \
		echo "  initramfs → ~/.loka/vm/initramfs.cpio.gz"; \
	else \
		echo "  ! build/initramfs.cpio.gz not found — run 'make kernel' first"; \
	fi

# Install LOKA locally from source
INSTALL_DIR ?= /usr/local/bin
install: build install-vm-assets
	@echo "==> Installing LOKA"
	sudo install -m 755 $(LOKA_CLI) $(INSTALL_DIR)/loka
	sudo install -m 755 $(LOKAD) $(INSTALL_DIR)/lokad
	sudo install -m 755 $(LOKA_SUPERVISOR) $(INSTALL_DIR)/loka-supervisor
ifeq ($(UNAME_S),Linux)
	@# On Linux, also install Cloud Hypervisor if not present.
	@if ! command -v cloud-hypervisor >/dev/null 2>&1; then \
		echo "  Cloud Hypervisor not found — installing..."; \
		$(MAKE) install-cloud-hypervisor; \
	else \
		echo "  cloud-hypervisor already installed"; \
	fi
endif
	@echo "  LOKA installed. Run: lokad"

# Uninstall LOKA
uninstall:
	@echo "==> Uninstalling LOKA"
	-sudo rm -f $(INSTALL_DIR)/loka $(INSTALL_DIR)/lokad $(INSTALL_DIR)/loka-supervisor
	-rm -rf $(HOME)/.loka
	@echo "  LOKA uninstalled"

# Build custom Linux kernel for lokavm
kernel:
	bash scripts/build-kernel-lokavm.sh

# Check kernel.org for latest stable and update pinned version
kernel-update:
	@echo "==> Checking latest stable kernel..."
	@LATEST=$$(curl -s https://www.kernel.org/ | grep -oP 'linux-\K[0-9]+\.[0-9]+\.[0-9]+' | head -1) && \
	echo "    Latest: $$LATEST" && \
	sed -i.bak "s/^PINNED_VERSION=.*/PINNED_VERSION=\"$$LATEST\"/" scripts/build-kernel-lokavm.sh && \
	rm -f scripts/build-kernel-lokavm.sh.bak && \
	echo "    Updated to $$LATEST"

# Run E2E test suite
e2e-test: build
	bash scripts/e2e-test.sh

# Lint
lint:
	golangci-lint run ./...

# Format
fmt:
	$(GO) fmt ./...
	goimports -w .

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)

# Help
help:
	@echo "LOKA - Session-Based MicroVM Execution OS for AI Agents"
	@echo ""
	@echo "Targets:"
	@echo "  build                Build all binaries for current platform"
	@echo "  build-linux          Cross-compile for Linux (amd64 + arm64)"
	@echo "  build-linux-amd64    Cross-compile for Linux x86_64"
	@echo "  build-linux-arm64    Cross-compile for Linux ARM64"
	@echo "  build-all            Build for all platforms"
	@echo "  install              Build + install locally"
	@echo "  install-cloud-hypervisor  Install Cloud Hypervisor (Linux VMM)"
	@echo "  install-vm-assets    Install kernel + initramfs to ~/.loka/vm/"
	@echo "  uninstall            Remove LOKA and all data"
	@echo "  e2e-test             Run E2E test suite"
	@echo "  kernel               Build custom Linux kernel for lokavm"
	@echo "  kernel-update        Update pinned kernel version to latest stable"
	@echo "  proto                Generate protobuf code"
	@echo "  test                 Run all tests (unit)"
	@echo "  lint                 Run linter"
	@echo "  fmt                  Format code"
	@echo "  clean                Remove build artifacts"
	@echo "  help                 Show this help"
