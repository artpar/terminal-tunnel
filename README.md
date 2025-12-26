# Terminal Tunnel

P2P terminal sharing with end-to-end encryption. Share your terminal from anywhere - no signup, no relay servers, just direct encrypted connections.

## Features

- **Zero setup** - Single binary, no dependencies
- **Daemon mode** - Background service manages multiple concurrent sessions
- **Fully P2P** - Direct WebRTC connection, your data never touches third-party servers
- **E2E encrypted** - Password-derived keys using Argon2id + NaCl SecretBox
- **Cross-NAT** - Works across different networks with multiple fallback modes
- **Mobile friendly** - Access from any device with a modern browser
- **Web client** - Use the hosted client at [artpar.github.io/terminal-tunnel](https://artpar.github.io/terminal-tunnel/)

## Quick Start

```bash
# Start the daemon
tt daemon start

# Start a terminal session
tt start -p mysecretpassword

# Output:
# Session started:
#   ID:       abc123
#   Code:     XYZ789
#   Password: mysecretpassword
#   URL:      https://artpar.github.io/terminal-tunnel/?c=XYZ789
```

Open the URL in any browser, enter the password, and you're connected!

```bash
# List active sessions
tt list

# Check daemon status
tt status

# Stop a session
tt stop XYZ789

# Stop the daemon
tt daemon stop
```

## Installation

### From Releases

Download from [Releases](https://github.com/artpar/terminal-tunnel/releases):

```bash
# Linux/macOS
tar -xzf terminal-tunnel-*.tar.gz
chmod +x tt
sudo mv tt /usr/local/bin/
```

### From Source

```bash
go install github.com/artpar/terminal-tunnel/cmd/terminal-tunnel@latest
```

## Command Reference

### Daemon Commands

```bash
tt daemon start    # Start daemon in background
tt daemon stop     # Stop daemon gracefully (terminates all sessions)
```

### Session Commands

```bash
tt start [flags]   # Start a new terminal session
tt stop <id|code>  # Stop a specific session
tt list            # List all active sessions
tt status          # Show daemon and session status
```

#### `tt start` Flags

| Flag | Description |
|------|-------------|
| `-p, --password` | Password for E2E encryption (auto-generated if not provided) |
| `-s, --shell` | Shell to use (default: $SHELL or /bin/sh) |

### Relay Command

```bash
tt relay [flags]   # Run a signaling relay server

Flags:
  --port int       Port for relay server (default: 8765)
```

## Usage Examples

### Basic Usage

```bash
# Start daemon (runs in background)
tt daemon start
# Daemon started (PID 12345)

# Start a session with auto-generated password
tt start
# Session started:
#   ID:       slUKah4FcXU
#   Code:     QTDAS2
#   Password: rAnDoMpAsSwOrD
#   URL:      https://artpar.github.io/terminal-tunnel/?c=QTDAS2

# Start a session with custom password
tt start -p mypassword -s /bin/zsh

# List all sessions
tt list
# ID           CODE    STATUS   SHELL    CREATED
# slUKah4FcXU  QTDAS2  waiting  /bin/sh  2 mins ago
# fFUq7tzPUkA  TBDZKF  connected /bin/zsh just now

# Check status
tt status
# Daemon: running (PID 12345, uptime 10m)
# Sessions: 2 total, 1 connected

# Stop a session by code
tt stop QTDAS2
# Session QTDAS2 stopped

# Stop daemon (terminates all sessions)
tt daemon stop
# Daemon stopped (1 sessions terminated)
```

### Running Multiple Sessions

```bash
# Start multiple sessions for different purposes
tt start -p dev-session     # Development
tt start -p demo-session    # Demo/presentation
tt start -p support-session # Remote support

# List all
tt list
# ID           CODE    STATUS   SHELL    CREATED
# abc123       DEV001  waiting  /bin/zsh just now
# def456       DEMO02  waiting  /bin/zsh just now
# ghi789       SUP003  waiting  /bin/zsh just now
```

### Self-Hosted Relay

For environments where the default relay isn't accessible:

```bash
# On your server - start the relay
tt relay --port 8765

# Sessions will automatically use the relay for signaling
# (set RELAY_URL environment variable to customize)
```

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  DAEMON (tt daemon start)                                        │
│  ~/.tt/tt.sock (Unix socket)                                     │
├──────────────────────────────────────────────────────────────────┤
│                     SessionManager                               │
│         ┌──────────────┬──────────────┬──────────────┐          │
│         │   Session 1  │   Session 2  │   Session 3  │          │
│         │  PTY+WebRTC  │  PTY+WebRTC  │  PTY+WebRTC  │          │
│         └──────────────┴──────────────┴──────────────┘          │
└──────────────────────────────────────────────────────────────────┘
           │                    │                    │
           │         WebRTC P2P (DTLS + E2E)         │
           │                    │                    │
┌──────────────────────────────────────────────────────────────────┐
│  CLIENTS (Browsers)                                              │
│  https://artpar.github.io/terminal-tunnel/                       │
├──────────────────────────────────────────────────────────────────┤
│  1. Load xterm.js terminal emulator                              │
│  2. Receive offer, prompt for password                           │
│  3. Derive encryption key (Argon2id)                             │
│  4. Create answer, complete WebRTC handshake                     │
│  5. Bridge: xterm.js ↔ Encrypted DataChannel                     │
└──────────────────────────────────────────────────────────────────┘
```

### State Directory

```
~/.tt/
├── tt.pid          # Daemon PID file
├── tt.sock         # Unix socket for IPC
└── sessions/       # Session state files
    └── XYZ789.json # Session state (keyed by short code)
```

## Testing

### Manual Testing

```bash
# 1. Build the binary
go build -o tt ./cmd/terminal-tunnel/

# 2. Start the daemon
./tt daemon start

# 3. Start a session
./tt start -p testpassword
# Note the Code and URL

# 4. Open URL in browser, enter password
# You should see a terminal

# 5. Test the connection
# Type commands in browser, see output
# Try resizing browser window

# 6. Test session management
./tt list
./tt status

# 7. Test disconnection/reconnection
# Close browser tab, reopen URL
# Session should reconnect

# 8. Cleanup
./tt stop <code>
./tt daemon stop
```

### Testing Multiple Sessions

```bash
# Start daemon
./tt daemon start

# Start 3 sessions
./tt start -p pass1
./tt start -p pass2
./tt start -p pass3

# Verify all are listed
./tt list

# Connect to each from different browser tabs
# Verify all work independently

# Stop one session
./tt stop <code>

# Verify it's removed from list
./tt list

# Stop daemon (should terminate remaining sessions)
./tt daemon stop
```

### Testing Connection Resilience

```bash
# Start daemon and session
./tt daemon start
./tt start -p test

# Connect from browser
# Verify terminal works

# Test network interruption:
# 1. Disable network briefly
# 2. Re-enable network
# 3. Session should auto-reconnect

# Test server resilience:
# 1. Kill daemon: kill $(cat ~/.tt/tt.pid)
# 2. Restart daemon: ./tt daemon start
# 3. Start new session (old sessions need manual restart)
```

## Security

### Encryption

| Layer | Protection |
|-------|------------|
| Transport | WebRTC DTLS (mandatory) |
| Application | NaCl SecretBox (E2E on top of DTLS) |

### Key Derivation

```
Argon2id(password, salt, time=3, memory=64MB, threads=4) → 256-bit key
```

- Random 16-byte salt per session
- Key derived independently on both ends
- Password never transmitted

### What's Protected

- All terminal I/O encrypted end-to-end
- Even if DTLS compromised, data remains encrypted
- Relay server (if used) only sees encrypted signaling data

## NAT Traversal

| Method | When Used |
|--------|-----------|
| STUN | Discovers public IP |
| UPnP/NAT-PMP | Automatic port forwarding |
| WebRTC ICE | Hole-punching for most NATs |
| Relay | Signaling only, data stays P2P |

### Limitations

- **Symmetric NAT on both sides**: May require TURN (not implemented)
- **Strict firewalls**: May block UDP
- **No TURN**: No data relay, keeps it truly P2P

## Platform Support

| Platform | Status |
|----------|--------|
| Linux amd64/arm64/armv7 | Full support |
| macOS amd64/arm64 | Full support |
| FreeBSD amd64 | Full support |
| Windows amd64/arm64 | Requires WSL |

## Building

```bash
# Build for current platform
make build

# Build for all platforms (static binaries)
make build-all

# Create release archives with checksums
make release

# Install to /usr/local/bin
make install

# See all targets
make help
```

### Static Builds

All builds use `CGO_ENABLED=0` for fully static binaries with no external dependencies.

| Platform | Architecture | Binary |
|----------|--------------|--------|
| Linux | amd64, arm64, armv7 | `tt-linux-{arch}` |
| macOS | amd64, arm64 | `tt-darwin-{arch}` |
| Windows | amd64, arm64 | `tt-windows-{arch}.exe` |
| FreeBSD | amd64 | `tt-freebsd-amd64` |

## CI/CD

GitHub Actions automatically:
- Runs tests on every push and PR
- Builds static binaries for all platforms
- Creates releases with archives and checksums when tags are pushed

### Creating a Release

```bash
# Tag a new version
git tag v1.0.0
git push origin v1.0.0

# GitHub Actions will automatically:
# 1. Run tests
# 2. Build all platform binaries
# 3. Create archives (tar.gz for Unix, zip for Windows)
# 4. Generate SHA256 checksums
# 5. Create GitHub Release with all artifacts
```

### Release Artifacts

Each release includes:
- `tt-{version}-linux-amd64.tar.gz`
- `tt-{version}-linux-arm64.tar.gz`
- `tt-{version}-linux-armv7.tar.gz`
- `tt-{version}-darwin-amd64.tar.gz`
- `tt-{version}-darwin-arm64.tar.gz`
- `tt-{version}-windows-amd64.zip`
- `tt-{version}-windows-arm64.zip`
- `tt-{version}-freebsd-amd64.tar.gz`
- `checksums-sha256.txt`

## License

MIT

## Acknowledgments

- [Pion WebRTC](https://github.com/pion/webrtc) - Pure Go WebRTC
- [creack/pty](https://github.com/creack/pty) - PTY handling
- [xterm.js](https://xtermjs.org/) - Browser terminal
- [Argon2](https://github.com/P-H-C/phc-winner-argon2) - Password hashing
