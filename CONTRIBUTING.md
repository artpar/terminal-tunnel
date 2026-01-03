# Contributing to Terminal Tunnel

Thank you for your interest in contributing to Terminal Tunnel! This guide will help you get started.

## Quick Start

```bash
# Clone the repo
git clone https://github.com/artpar/terminal-tunnel.git
cd terminal-tunnel

# Build
make build

# Run tests
make test

# Run API tests
npm install -g @usebruno/cli
make test-api
```

## Development Setup

### Prerequisites

- **Go 1.21+** - [Install Go](https://go.dev/doc/install)
- **Node.js 18+** - For API testing (optional)
- **Bruno CLI** - For API testing (optional): `npm install -g @usebruno/cli`

### Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Install to /usr/local/bin
make install
```

### Running Tests

```bash
# Go unit tests
make test

# API integration tests (requires Bruno CLI)
make test-api

# Manual testing
./tt daemon start
./tt start -p testpassword
# Open the URL in browser, verify terminal works
./tt daemon stop
```

## Project Structure

```
terminal-tunnel/
├── cmd/terminal-tunnel/     # CLI entry point (main.go, commands)
├── internal/
│   ├── daemon/              # Background service management
│   ├── session/             # Terminal session handling
│   ├── signaling/           # WebRTC signaling (relay, shortcode)
│   ├── server/              # HTTP server for local signaling
│   ├── crypto/              # Encryption (Argon2id, NaCl)
│   └── pty/                 # PTY abstraction (Unix/Windows)
├── docs/                    # Web client (served via GitHub Pages)
├── relay-worker/            # Cloudflare Worker for relay
├── examples/
│   └── courier-collection/  # Bruno API test collection
├── services/                # Systemd/launchd service files
├── build/                   # Build scripts and configs
└── test/                    # Test utilities
```

## Making Changes

### Code Style

- Follow standard Go conventions (`go fmt`, `go vet`)
- Keep functions focused and well-documented
- Add tests for new functionality
- Update documentation when changing behavior

### Commit Messages

Use clear, descriptive commit messages:

```
Add session recording feature

- Implement asciicast v2 format writer
- Add --record flag to start command
- Add play command for playback
```

### Pull Request Process

1. **Fork** the repository
2. **Create a branch** for your feature: `git checkout -b feature/my-feature`
3. **Make changes** and add tests
4. **Run tests**: `make test && make test-api`
5. **Commit** with a clear message
6. **Push** to your fork: `git push origin feature/my-feature`
7. **Open a PR** against `main`

### PR Guidelines

- Keep PRs focused on a single feature or fix
- Include tests for new functionality
- Update documentation if needed
- Ensure all CI checks pass

## Areas for Contribution

### Good First Issues

- Documentation improvements
- Additional test coverage
- CLI help text improvements
- Bug fixes

### Feature Ideas

- Additional shell support
- Session sharing/handoff
- Custom themes for web client
- Notification system
- Session history

### Infrastructure

- CI/CD improvements
- Package manager submissions
- Performance benchmarks

## Testing

### Unit Tests

```bash
make test
```

### API Tests

The API test collection uses [Bruno](https://www.usebruno.com/) and tests all relay server endpoints:

```bash
# Install Bruno CLI
npm install -g @usebruno/cli

# Run tests (starts local relay, runs tests, stops relay)
make test-api
```

### Manual Testing Checklist

- [ ] `tt start -p test` creates session
- [ ] Browser can connect with password
- [ ] Terminal input/output works
- [ ] Window resize works
- [ ] `tt list` shows session
- [ ] `tt stop <code>` stops session
- [ ] `tt daemon stop` cleans up

## Getting Help

- **Issues**: [GitHub Issues](https://github.com/artpar/terminal-tunnel/issues)
- **Discussions**: [GitHub Discussions](https://github.com/artpar/terminal-tunnel/discussions)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
