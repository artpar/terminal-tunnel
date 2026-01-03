BINARY_NAME=tt
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

# Build directories
BUILD_DIR=build
DIST_DIR=dist

.PHONY: all build clean test test-api test-ws test-all build-all release install packages

all: build

# Build for current platform
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/terminal-tunnel

# Run tests
test:
	go test -v ./...

# Run API tests with Bruno
test-api: build
	@which bru > /dev/null || (echo "Bruno CLI not found. Install: npm install -g @usebruno/cli" && exit 1)
	@echo "Starting relay server on port 8765..."
	@./$(BINARY_NAME) relay --port 8765 & echo $$! > .relay.pid
	@sleep 2
	@echo ""
	@if curl -s http://localhost:8765/health > /dev/null 2>&1; then \
		echo "Relay server is running"; \
	else \
		echo "Failed to start relay server"; \
		kill $$(cat .relay.pid) 2>/dev/null || true; \
		rm -f .relay.pid; \
		exit 1; \
	fi
	@echo ""
	@echo "Running Bruno API tests..."
	@echo "========================================"
	@cd examples/courier-collection && bru run --env Local 2>&1; \
		TEST_EXIT=$$?; \
		kill $$(cat ../../.relay.pid) 2>/dev/null || true; \
		rm -f ../../.relay.pid; \
		exit $$TEST_EXIT
	@echo "========================================"
	@echo "API tests complete"

# Run WebSocket tests
test-ws: build
	@which node > /dev/null || (echo "Node.js not found. Install from https://nodejs.org" && exit 1)
	@echo "Installing WebSocket test dependencies..."
	@cd examples/courier-collection/scripts && npm install --silent
	@echo ""
	@echo "Starting relay server on port 8765..."
	@./$(BINARY_NAME) relay --port 8765 & echo $$! > .relay.pid
	@sleep 2
	@if curl -s http://localhost:8765/health > /dev/null 2>&1; then \
		echo "Relay server is running"; \
	else \
		echo "Failed to start relay server"; \
		kill $$(cat .relay.pid) 2>/dev/null || true; \
		rm -f .relay.pid; \
		exit 1; \
	fi
	@echo ""
	@echo "Running WebSocket tests..."
	@echo "========================================"
	@cd examples/courier-collection/scripts && node websocket-test.js 2>&1; \
		TEST_EXIT=$$?; \
		kill $$(cat ../../../.relay.pid) 2>/dev/null || true; \
		rm -f ../../../.relay.pid; \
		exit $$TEST_EXIT
	@echo "========================================"
	@echo "WebSocket tests complete"

# Run all API tests (Bruno + WebSocket)
test-all: build
	@which bru > /dev/null || (echo "Bruno CLI not found. Install: npm install -g @usebruno/cli" && exit 1)
	@which node > /dev/null || (echo "Node.js not found. Install from https://nodejs.org" && exit 1)
	@cd examples/courier-collection/scripts && npm install --silent
	@echo "Starting relay server on port 8765..."
	@./$(BINARY_NAME) relay --port 8765 & echo $$! > .relay.pid
	@sleep 2
	@if curl -s http://localhost:8765/health > /dev/null 2>&1; then \
		echo "Relay server is running"; \
	else \
		echo "Failed to start relay server"; \
		kill $$(cat .relay.pid) 2>/dev/null || true; \
		rm -f .relay.pid; \
		exit 1; \
	fi
	@echo ""
	@echo "========================================"
	@echo "Running Bruno API tests..."
	@echo "========================================"
	@cd examples/courier-collection && bru run --env Local 2>&1 || true
	@echo ""
	@echo "========================================"
	@echo "Running WebSocket tests..."
	@echo "========================================"
	@cd examples/courier-collection/scripts && node websocket-test.js 2>&1; \
		TEST_EXIT=$$?; \
		kill $$(cat ../../../.relay.pid) 2>/dev/null || true; \
		rm -f ../../../.relay.pid; \
		exit $$TEST_EXIT
	@echo ""
	@echo "All tests complete"

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR) $(BINARY_NAME)

# Cross-platform static builds
build-all: clean
	mkdir -p $(BUILD_DIR)

	# Linux AMD64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/terminal-tunnel

	# Linux ARM64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/terminal-tunnel

	# Linux ARMv7
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-armv7 ./cmd/terminal-tunnel

	# macOS AMD64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/terminal-tunnel

	# macOS ARM64 (Apple Silicon)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/terminal-tunnel

	# Windows AMD64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/terminal-tunnel

	# Windows ARM64
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.exe ./cmd/terminal-tunnel

	# FreeBSD AMD64
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-freebsd-amd64 ./cmd/terminal-tunnel

	@echo ""
	@echo "Build complete. Binaries in $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/

# Create release archives with checksums
release: build-all
	mkdir -p $(DIST_DIR)

	# Create tar.gz archives for Unix
	cd $(BUILD_DIR) && for f in $(BINARY_NAME)-linux-* $(BINARY_NAME)-darwin-* $(BINARY_NAME)-freebsd-*; do \
		if [ -f "$$f" ]; then \
			cp "$$f" $(BINARY_NAME) && chmod +x $(BINARY_NAME) && \
			tar -czf ../$(DIST_DIR)/$$f-$(VERSION).tar.gz $(BINARY_NAME) && \
			rm $(BINARY_NAME); \
		fi \
	done

	# Create zip archives for Windows
	cd $(BUILD_DIR) && for f in $(BINARY_NAME)-windows-*.exe; do \
		if [ -f "$$f" ]; then \
			name=$${f%.exe}; \
			cp "$$f" $(BINARY_NAME).exe && \
			zip -q ../$(DIST_DIR)/$$name-$(VERSION).zip $(BINARY_NAME).exe && \
			rm $(BINARY_NAME).exe; \
		fi \
	done

	# Generate checksums
	cd $(DIST_DIR) && sha256sum * > checksums-sha256.txt

	@echo ""
	@echo "Release archives in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/
	@echo ""
	@echo "Checksums:"
	@cat $(DIST_DIR)/checksums-sha256.txt

# Build Linux packages (DEB, RPM, APK) using nfpm
packages: build-all
	@which nfpm > /dev/null || (echo "nfpm not found. Install: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest" && exit 1)
	mkdir -p $(DIST_DIR)

	# AMD64 packages (use envsubst for variable expansion in nfpm.yaml)
	GOARCH=amd64 VERSION=$(VERSION) envsubst < nfpm.yaml > /tmp/nfpm-amd64.yaml
	nfpm pkg --config /tmp/nfpm-amd64.yaml --packager deb --target $(DIST_DIR)/tt_$(VERSION)_amd64.deb
	nfpm pkg --config /tmp/nfpm-amd64.yaml --packager rpm --target $(DIST_DIR)/tt-$(VERSION).x86_64.rpm
	nfpm pkg --config /tmp/nfpm-amd64.yaml --packager apk --target $(DIST_DIR)/tt_$(VERSION)_x86_64.apk

	# ARM64 packages
	GOARCH=arm64 VERSION=$(VERSION) envsubst < nfpm.yaml > /tmp/nfpm-arm64.yaml
	nfpm pkg --config /tmp/nfpm-arm64.yaml --packager deb --target $(DIST_DIR)/tt_$(VERSION)_arm64.deb
	nfpm pkg --config /tmp/nfpm-arm64.yaml --packager rpm --target $(DIST_DIR)/tt-$(VERSION).aarch64.rpm
	nfpm pkg --config /tmp/nfpm-arm64.yaml --packager apk --target $(DIST_DIR)/tt_$(VERSION)_aarch64.apk

	@echo ""
	@echo "Packages built in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/*.deb $(DIST_DIR)/*.rpm $(DIST_DIR)/*.apk 2>/dev/null || true

# Install locally
install: build
	sudo cp $(BINARY_NAME) /usr/local/bin/
	@echo "Installed $(BINARY_NAME) to /usr/local/bin/"

# Uninstall
uninstall:
	sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Uninstalled $(BINARY_NAME) from /usr/local/bin/"

# Development: run daemon in foreground
dev:
	go run ./cmd/terminal-tunnel daemon foreground

# Development: build and start daemon
run: build
	./$(BINARY_NAME) daemon start

# Show help
help:
	@echo "Terminal Tunnel (tt) - P2P terminal sharing"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build      Build for current platform"
	@echo "  build-all  Build for all platforms (static)"
	@echo "  release    Create release archives with checksums"
	@echo "  packages   Build DEB/RPM/APK packages (requires nfpm)"
	@echo "  test       Run Go tests"
	@echo "  test-api   Run Bruno HTTP API tests (requires: npm i -g @usebruno/cli)"
	@echo "  test-ws    Run WebSocket tests (requires: node)"
	@echo "  test-all   Run all API tests (Bruno + WebSocket)"
	@echo "  install    Install to /usr/local/bin"
	@echo "  uninstall  Remove from /usr/local/bin"
	@echo "  clean      Remove build artifacts"
	@echo "  dev        Run daemon in foreground (for development)"
	@echo "  run        Build and start daemon"
	@echo "  help       Show this help"

.DEFAULT_GOAL := build
