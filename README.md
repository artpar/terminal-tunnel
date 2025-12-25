# Terminal Tunnel

P2P terminal sharing with end-to-end encryption. Share your terminal from anywhere - no signup, no relay servers, just direct encrypted connections.

## Features

- **Zero setup** - Single binary, no dependencies
- **Fully P2P** - Direct WebRTC connection, your data never touches third-party servers
- **E2E encrypted** - Password-derived keys using Argon2id + NaCl SecretBox
- **Cross-NAT** - Works across different networks with multiple fallback modes
- **Mobile friendly** - Access from any device with a modern browser
- **Web client** - Use the hosted client at [artpar.github.io/terminal-tunnel](https://artpar.github.io/terminal-tunnel/)

## Quick Start

```bash
# Start sharing your terminal
./terminal-tunnel serve -p mysecretpassword

# Output:
# Share this link: http://203.0.113.5:54321
# Password: mysecretpassword
```

Open the link in any browser, enter the password, and you're connected!

## Installation

### From Releases

Download from [Releases](https://github.com/artpar/terminal-tunnel/releases):

```bash
# Linux/macOS
tar -xzf terminal-tunnel-*.tar.gz
chmod +x terminal-tunnel
sudo mv terminal-tunnel /usr/local/bin/
```

### From Source

```bash
go install github.com/artpar/terminal-tunnel/cmd/terminal-tunnel@latest
```

## Connection Modes

Terminal Tunnel automatically selects the best connection method:

### 1. Direct Mode (Default)

Works when your network allows incoming connections (UPnP enabled or port forwarded):

```bash
./terminal-tunnel serve -p mypassword
```

### 2. Relay Mode

Use a self-hosted relay server for signaling (data still flows P2P):

```bash
# On your server - start the relay
./terminal-tunnel relay --port 8765

# On your machine - use the relay
./terminal-tunnel serve -p mypassword --relay ws://your-server:8765
```

### 3. Manual Mode

Works everywhere - exchange codes manually via any channel (email, chat, etc):

```bash
./terminal-tunnel serve -p mypassword --manual
```

Then:
1. Copy the connection code displayed
2. Open [artpar.github.io/terminal-tunnel](https://artpar.github.io/terminal-tunnel/)
3. Paste the code and enter password
4. Copy the answer code back to terminal

## Web Client

The web client is hosted at **https://artpar.github.io/terminal-tunnel/**

It works in three modes:
- **Auto** - When opened from a direct link
- **Relay** - When connecting via a relay server
- **Manual** - Paste connection codes for pure P2P

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  HOST                                                            │
│  ./terminal-tunnel serve -p secret                               │
├──────────────────────────────────────────────────────────────────┤
│  1. Start PTY (bash/zsh)                                         │
│  2. Create WebRTC offer with ICE candidates                      │
│  3. Share offer via HTTP / Relay / Manual                        │
│  4. Receive answer, establish P2P DataChannel                    │
│  5. Bridge: PTY ↔ Encrypted DataChannel                          │
└──────────────────────────────────────────────────────────────────┘
                              │
                    WebRTC P2P (DTLS + E2E)
                              │
┌──────────────────────────────────────────────────────────────────┐
│  CLIENT (Browser)                                                │
│  https://artpar.github.io/terminal-tunnel/                       │
├──────────────────────────────────────────────────────────────────┤
│  1. Load xterm.js terminal emulator                              │
│  2. Receive offer, prompt for password                           │
│  3. Derive encryption key (Argon2id)                             │
│  4. Create answer, complete WebRTC handshake                     │
│  5. Bridge: xterm.js ↔ Encrypted DataChannel                     │
└──────────────────────────────────────────────────────────────────┘
```

## Command Reference

### `serve` - Share your terminal

```bash
terminal-tunnel serve [flags]

Flags:
  -p, --password string   Password for E2E encryption (required)
  -s, --shell string      Shell to use (default: $SHELL)
  -t, --timeout duration  Connection timeout (default: 5m)
      --relay string      WebSocket relay URL for signaling
      --no-relay          Disable relay, use manual if UPnP fails
      --manual            Force manual mode (QR/copy-paste)
```

### `relay` - Run a signaling relay

```bash
terminal-tunnel relay [flags]

Flags:
      --port int   Port for relay server (default: 8765)
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
| Manual | Works through any NAT |

### Limitations

- **Symmetric NAT on both sides**: Use relay or manual mode
- **Strict firewalls**: May block UDP, use manual mode
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
make build          # Current platform
make build-all      # All platforms
make release        # Create archives
```

## License

MIT

## Acknowledgments

- [Pion WebRTC](https://github.com/pion/webrtc) - Pure Go WebRTC
- [creack/pty](https://github.com/creack/pty) - PTY handling
- [xterm.js](https://xtermjs.org/) - Browser terminal
- [Argon2](https://github.com/P-H-C/phc-winner-argon2) - Password hashing
