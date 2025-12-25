# Terminal Tunnel

P2P terminal sharing with end-to-end encryption. Access your terminal from any device, including Android.

## Features

- **One command startup** - Single command generates a shareable link
- **Fully P2P** - No third-party relay servers, direct WebRTC connection
- **E2E encrypted** - Password-derived keys using Argon2id + NaCl SecretBox
- **Cross-platform** - Works on Linux, macOS, Windows (WSL), FreeBSD
- **Mobile friendly** - Access from any device with a modern browser
- **NAT traversal** - Automatic UPnP port forwarding with manual fallback

## Installation

### From Releases

Download the latest binary for your platform from [Releases](https://github.com/artpar/terminal-tunnel/releases).

```bash
# Linux/macOS
tar -xzf terminal-tunnel-*.tar.gz
chmod +x terminal-tunnel
sudo mv terminal-tunnel /usr/local/bin/

# Or run directly
./terminal-tunnel serve --password mysecret
```

### From Source

```bash
go install github.com/artpar/terminal-tunnel/cmd/terminal-tunnel@latest
```

Or clone and build:

```bash
git clone https://github.com/artpar/terminal-tunnel.git
cd terminal-tunnel
make build
```

## Usage

### Start a terminal session

```bash
terminal-tunnel serve --password mysecret
```

Output:
```
Terminal Tunnel Server
======================
Share this link: http://203.0.113.5:54321
Password: mysecret (share separately!)

Waiting for connection...
```

### Connect from another device

1. Open the link in a browser (Chrome, Firefox, Safari)
2. Enter the password
3. Terminal appears - you're connected!

### Options

```bash
terminal-tunnel serve [flags]

Flags:
  -p, --password string   Password for E2E encryption (required)
  -s, --shell string      Shell to use (default: $SHELL or /bin/sh)
      --port int          HTTP port for signaling (default: random)
  -h, --help              Help for serve
```

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│  HOST: terminal-tunnel serve --password secret                  │
├─────────────────────────────────────────────────────────────────┤
│  1. Start PTY (bash/zsh)                                        │
│  2. Create WebRTC offer                                         │
│  3. Start HTTP server for signaling                             │
│  4. Display shareable link                                      │
│  5. Wait for client answer                                      │
│  6. Establish P2P DataChannel                                   │
│  7. Bridge: PTY ↔ Encrypted DataChannel                         │
└─────────────────────────────────────────────────────────────────┘
                              │
                    WebRTC P2P (DTLS + E2E)
                              │
┌─────────────────────────────────────────────────────────────────┐
│  CLIENT: Browser opens link                                     │
├─────────────────────────────────────────────────────────────────┤
│  1. Load xterm.js terminal                                      │
│  2. Prompt for password                                         │
│  3. Derive encryption key (Argon2id)                            │
│  4. Complete WebRTC handshake                                   │
│  5. Establish P2P DataChannel                                   │
│  6. Bridge: xterm.js ↔ Encrypted DataChannel                    │
└─────────────────────────────────────────────────────────────────┘
```

## Security

### Encryption Layers

1. **Transport**: WebRTC DTLS (mandatory, handles key exchange)
2. **Application**: NaCl SecretBox on top of DTLS for E2E encryption

### Key Derivation

```
Argon2id(password, salt, time=3, memory=64MB, threads=4) → 256-bit key
```

- Salt is random 16 bytes, generated per session
- Key never transmitted - derived independently on both sides

### What's Protected

- All terminal I/O is encrypted end-to-end
- Password never sent over the network
- Even if DTLS were compromised, data remains encrypted

## NAT Traversal

The tool uses multiple strategies to work across networks:

1. **STUN** - Discovers public IP via Google's STUN servers
2. **UPnP/NAT-PMP** - Attempts automatic port forwarding
3. **WebRTC ICE** - Hole-punching for most NAT types

### Limitations

- **Symmetric NAT on both sides**: Won't work (no TURN relay)
- **Strict firewalls**: May block UDP traffic
- **Carrier-grade NAT (CGNAT)**: UPnP won't help, but WebRTC may still work

### Fallback

If the link isn't reachable, the tool displays a QR code and offer text for manual exchange.

## Platform Support

| Platform | Status |
|----------|--------|
| Linux amd64 | Full support |
| Linux arm64 | Full support |
| Linux armv7 | Full support |
| macOS amd64 | Full support |
| macOS arm64 | Full support |
| Windows amd64 | Requires WSL |
| Windows arm64 | Requires WSL |
| FreeBSD amd64 | Full support |

Windows note: Native PTY not supported. Run inside WSL for full functionality.

## Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Create release archives
make release
```

## License

MIT

## Acknowledgments

- [Pion WebRTC](https://github.com/pion/webrtc) - Pure Go WebRTC implementation
- [creack/pty](https://github.com/creack/pty) - PTY handling
- [xterm.js](https://xtermjs.org/) - Terminal emulator for the browser
- [Argon2](https://github.com/P-H-C/phc-winner-argon2) - Password hashing
