# Development Guide

This document covers the architecture, development setup, and contribution guidelines for Terminal Tunnel.

## Project Structure

```
terminal-tunnel/
├── cmd/
│   └── terminal-tunnel/
│       └── main.go              # CLI entry point (cobra commands)
├── internal/
│   ├── client/
│   │   └── client.go            # CLI client for daemon IPC
│   ├── crypto/
│   │   ├── keys.go              # Argon2id key derivation
│   │   ├── keys_test.go
│   │   ├── secretbox.go         # NaCl SecretBox encryption
│   │   └── secretbox_test.go
│   ├── daemon/
│   │   ├── daemon.go            # Main daemon loop, socket listener
│   │   ├── pidfile.go           # PID file and state directory management
│   │   ├── protocol.go          # JSON-RPC types and methods
│   │   ├── session_manager.go   # Session lifecycle management
│   │   └── state.go             # Session state persistence
│   ├── protocol/
│   │   ├── messages.go          # Terminal protocol messages
│   │   ├── messages_test.go
│   │   ├── sdp.go               # SDP compression/encoding
│   │   └── sdp_test.go
│   ├── server/
│   │   ├── http.go              # HTTP signaling server
│   │   ├── http_test.go
│   │   ├── pty_unix.go          # Unix PTY implementation
│   │   ├── pty_windows.go       # Windows PTY (stub)
│   │   ├── pty_test.go
│   │   ├── server.go            # Main server orchestration
│   │   └── upnp.go              # UPnP port mapping
│   ├── signaling/
│   │   ├── manual.go            # Manual (QR/copy-paste) signaling
│   │   ├── relay.go             # WebSocket relay client
│   │   ├── relayserver/
│   │   │   └── server.go        # Relay server implementation
│   │   ├── shortcode.go         # Short code signaling client
│   │   └── types.go             # Signaling types and constants
│   ├── web/
│   │   ├── embed.go             # Embedded static files
│   │   └── static/              # Web client files
│   │       └── index.html
│   └── webrtc/
│       ├── datachannel.go       # Encrypted data channel
│       ├── peer.go              # WebRTC peer connection
│       └── peer_test.go
├── docs/
│   └── index.html               # GitHub Pages web client
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── DEVELOPMENT.md
```

## Architecture Overview

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                           CLI (main.go)                             │
│  tt daemon start/stop | tt start/stop/list/status | tt relay        │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
┌─────────────────────────────┐    ┌─────────────────────────────────┐
│     Daemon (daemon.go)      │    │    Relay Server (relayserver/)  │
│  - Unix socket listener     │    │  - WebSocket hub                │
│  - JSON-RPC handler         │    │  - Session routing              │
│  - Session manager          │    │  - Short code generation        │
└─────────────────────────────┘    └─────────────────────────────────┘
              │
              ▼
┌─────────────────────────────┐
│  SessionManager             │
│  - Start/stop sessions      │
│  - Track session state      │
│  - Persist to disk          │
└─────────────────────────────┘
              │
              ▼ (one per session)
┌─────────────────────────────┐
│     Server (server.go)      │
│  - WebRTC peer              │
│  - Signaling orchestration  │
│  - Connection loop          │
└─────────────────────────────┘
         │              │
         ▼              ▼
┌─────────────┐  ┌─────────────────┐
│ PTY Bridge  │  │ EncryptedChannel│
│ (pty_unix)  │  │ (datachannel)   │
│ - Shell     │  │ - NaCl encrypt  │
│ - I/O       │  │ - Message frame │
└─────────────┘  └─────────────────┘
```

### Data Flow

```
User Input (Browser)
        │
        ▼
┌─────────────────┐
│    xterm.js     │
│  (Web Client)   │
└────────┬────────┘
         │ plaintext
         ▼
┌─────────────────┐
│  Encrypt with   │
│  NaCl SecretBox │
└────────┬────────┘
         │ ciphertext
         ▼
┌─────────────────┐
│  WebRTC DTLS    │
│  DataChannel    │
└────────┬────────┘
         │ encrypted transport
         ▼
    ═══════════════
       Internet
    ═══════════════
         │
         ▼
┌─────────────────┐
│  WebRTC DTLS    │
│  DataChannel    │
└────────┬────────┘
         │ ciphertext
         ▼
┌─────────────────┐
│  Decrypt with   │
│  NaCl SecretBox │
└────────┬────────┘
         │ plaintext
         ▼
┌─────────────────┐
│    PTY/Shell    │
│    (Server)     │
└─────────────────┘
```

## Key Packages

### `internal/daemon`

The daemon manages the lifecycle of terminal sessions:

- **daemon.go**: Main loop accepting connections on Unix socket (`~/.tt/tt.sock`)
- **protocol.go**: JSON-RPC request/response types
- **session_manager.go**: Creates and tracks `ManagedSession` objects
- **state.go**: Persists session state to `~/.tt/sessions/`
- **pidfile.go**: PID file management for single-instance daemon

### `internal/server`

Core terminal sharing logic:

- **server.go**: Orchestrates signaling, WebRTC, and PTY
- **pty_unix.go**: PTY creation and management (Unix)
- **http.go**: HTTP signaling server (for direct connections)
- **upnp.go**: UPnP/NAT-PMP port mapping

### `internal/signaling`

Multiple signaling methods for SDP exchange:

- **shortcode.go**: HTTP API with 6-character codes (default)
- **relay.go**: WebSocket relay client
- **manual.go**: QR code / copy-paste fallback
- **relayserver/**: Self-hosted relay server

### `internal/crypto`

End-to-end encryption:

- **keys.go**: Argon2id key derivation from password + salt
- **secretbox.go**: NaCl SecretBox encrypt/decrypt

### `internal/webrtc`

WebRTC abstraction:

- **peer.go**: Pion WebRTC peer connection wrapper
- **datachannel.go**: Encrypted channel with message framing

## IPC Protocol

The daemon uses JSON-RPC over Unix socket:

### Request

```json
{
  "id": "unique-id",
  "method": "session.start",
  "params": {"password": "secret", "shell": "/bin/zsh"}
}
```

### Response

```json
{
  "id": "unique-id",
  "result": {"id": "abc123", "short_code": "XYZ789", "password": "secret"}
}
```

### Methods

| Method | Description |
|--------|-------------|
| `session.start` | Start a new terminal session |
| `session.stop` | Stop a session by ID or code |
| `session.list` | List all active sessions |
| `daemon.status` | Get daemon status and stats |
| `daemon.shutdown` | Gracefully stop daemon |

## Development Setup

### Prerequisites

- Go 1.21+
- Make (optional)

### Building

```bash
# Build for current platform
go build -o tt ./cmd/terminal-tunnel/

# Run tests
go test ./...

# Build for all platforms
make build-all
```

### Running Locally

```bash
# Terminal 1: Start daemon
./tt daemon start

# Terminal 2: Start session
./tt start -p test

# Open URL in browser to connect

# View logs (daemon runs in background)
# Check ~/.tt/ for state files
```

### Debugging

```bash
# Run daemon in foreground (see logs)
./tt daemon foreground

# Check daemon status
./tt status

# List sessions
./tt list

# Check state files
ls -la ~/.tt/
cat ~/.tt/sessions/*.json
```

## Testing

### Unit Tests

```bash
go test ./...
go test -v ./internal/crypto/...
go test -v ./internal/protocol/...
```

### Integration Testing

```bash
# Start daemon
./tt daemon start

# Create session
./tt start -p testpass

# Connect from browser, verify terminal works

# Test reconnection
# 1. Close browser tab
# 2. Reopen URL
# 3. Should reconnect to same session

# Test multiple sessions
./tt start -p pass1
./tt start -p pass2
./tt list  # Should show both

# Cleanup
./tt daemon stop
```

### Testing Relay Server

```bash
# Terminal 1: Start relay
./tt relay --port 8765

# Terminal 2: Set relay URL and start session
export RELAY_URL=ws://localhost:8765
./tt daemon start
./tt start -p test
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Error messages should be lowercase, no trailing punctuation
- Use context for cancellation where appropriate

## Adding New Features

### Adding a New CLI Command

1. Add command in `cmd/terminal-tunnel/main.go`
2. If daemon interaction needed, add method to `internal/daemon/protocol.go`
3. Implement handler in `internal/daemon/daemon.go`
4. Add client method in `internal/client/client.go`

### Adding a New Signaling Method

1. Create new file in `internal/signaling/`
2. Implement signaling interface
3. Add case in `server.go` `determineSignalingMethod()` and `Start()`

## CI/CD Pipeline

The project uses GitHub Actions for continuous integration and releases.

### Workflow: `.github/workflows/build.yml`

| Trigger | Action |
|---------|--------|
| Push to `main` | Run tests, build all platforms |
| Pull request | Run tests, build all platforms |
| Tag `v*` | Run tests, build, create release |

### Pipeline Stages

1. **Test**: Run `go test ./...`
2. **Build**: Build static binaries for all platforms (matrix build)
3. **Release** (tags only): Create archives, checksums, GitHub release

### Creating a Release

```bash
# 1. Update version in code if needed
# 2. Commit changes
git add .
git commit -m "Prepare release v1.0.0"

# 3. Create and push tag
git tag v1.0.0
git push origin main --tags

# 4. GitHub Actions will automatically create the release
```

### Build Matrix

| OS | Architecture | Static |
|----|--------------|--------|
| Linux | amd64 | Yes |
| Linux | arm64 | Yes |
| Linux | armv7 | Yes |
| macOS | amd64 | Yes |
| macOS | arm64 | Yes |
| Windows | amd64 | Yes |
| Windows | arm64 | Yes |
| FreeBSD | amd64 | Yes |

All builds use `CGO_ENABLED=0` for fully static binaries.

## Roadmap

### Completed

- [x] Daemon architecture with session management
- [x] Multiple concurrent sessions
- [x] Session persistence to disk
- [x] Short code signaling (default)
- [x] Auto-reconnect on disconnect
- [x] Keepalive/ping-pong for connection health

### Planned

- [ ] PTY reattachment after daemon restart
- [ ] Session timeout/cleanup
- [ ] Authentication for daemon IPC
- [ ] Windows native PTY support
- [ ] TURN server support for symmetric NAT

## Troubleshooting

### Daemon won't start

```bash
# Check if already running
cat ~/.tt/tt.pid
ps aux | grep tt

# Clean up stale files
rm ~/.tt/tt.pid ~/.tt/tt.sock
./tt daemon start
```

### Session won't connect

```bash
# Check session status
./tt list
./tt status

# Check relay connectivity
curl https://artpar.github.io/terminal-tunnel/

# Try with verbose output
./tt daemon foreground  # See connection logs
```

### WebRTC connection fails

- Check firewall settings (UDP required)
- Try from different network
- Check browser console for errors
- Verify STUN servers are accessible

## License

MIT
