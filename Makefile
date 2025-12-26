BINARY_NAME=tt
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

# Build directories
BUILD_DIR=build
DIST_DIR=dist

.PHONY: all build clean test build-all release install

all: build

# Build for current platform
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/terminal-tunnel

# Run tests
test:
	go test -v ./...

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
	@echo "  test       Run tests"
	@echo "  install    Install to /usr/local/bin"
	@echo "  uninstall  Remove from /usr/local/bin"
	@echo "  clean      Remove build artifacts"
	@echo "  dev        Run daemon in foreground (for development)"
	@echo "  run        Build and start daemon"
	@echo "  help       Show this help"

.DEFAULT_GOAL := build
