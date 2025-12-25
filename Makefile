BINARY_NAME=terminal-tunnel
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

# Build directories
BUILD_DIR=build
DIST_DIR=dist

.PHONY: all build clean test build-all

all: build

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/terminal-tunnel

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR) $(BINARY_NAME)

# Cross-platform static builds
build-all: clean
	mkdir -p $(DIST_DIR)

	# Linux AMD64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/terminal-tunnel

	# Linux ARM64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/terminal-tunnel

	# Linux ARMv7
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-armv7 ./cmd/terminal-tunnel

	# macOS AMD64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/terminal-tunnel

	# macOS ARM64 (Apple Silicon)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/terminal-tunnel

	# Windows AMD64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/terminal-tunnel

	# Windows ARM64
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.exe ./cmd/terminal-tunnel

	# FreeBSD AMD64
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-freebsd-amd64 ./cmd/terminal-tunnel

	@echo "Build complete. Binaries in $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/

# Create release archives
release: build-all
	mkdir -p $(DIST_DIR)

	# Create archives
	cd $(BUILD_DIR) && tar -czf ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz $(BINARY_NAME)-linux-amd64
	cd $(BUILD_DIR) && tar -czf ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz $(BINARY_NAME)-linux-arm64
	cd $(BUILD_DIR) && tar -czf ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-linux-armv7.tar.gz $(BINARY_NAME)-linux-armv7
	cd $(BUILD_DIR) && tar -czf ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz $(BINARY_NAME)-darwin-amd64
	cd $(BUILD_DIR) && tar -czf ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz $(BINARY_NAME)-darwin-arm64
	cd $(BUILD_DIR) && zip -q ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BINARY_NAME)-windows-amd64.exe
	cd $(BUILD_DIR) && zip -q ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-windows-arm64.zip $(BINARY_NAME)-windows-arm64.exe
	cd $(BUILD_DIR) && tar -czf ../$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-freebsd-amd64.tar.gz $(BINARY_NAME)-freebsd-amd64

	@echo "Release archives in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/

# Install locally
install: build
	cp $(BINARY_NAME) /usr/local/bin/

.DEFAULT_GOAL := build
