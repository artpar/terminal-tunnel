package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/artpar/terminal-tunnel/internal/server"
	"github.com/artpar/terminal-tunnel/internal/signaling/relayserver"
)

var (
	version = "0.1.0"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "terminal-tunnel",
	Short: "P2P terminal sharing with E2E encryption",
	Long: `Terminal Tunnel enables secure, peer-to-peer terminal sharing.

Run a terminal session that can be accessed from any device (including Android)
via a web browser. All traffic is end-to-end encrypted with a password you choose.

Example:
  terminal-tunnel serve --password mysecret
  terminal-tunnel serve  # auto-generates password`,
	Version: version,
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a terminal sharing session",
	Long: `Start a terminal sharing session that clients can connect to.

The server will:
1. Generate a WebRTC offer
2. Start an HTTP server for signaling
3. Attempt UPnP port mapping (if available)
4. Display a URL and QR code for clients
5. Wait for a client to connect
6. Bridge the terminal to the encrypted data channel`,
	RunE: runServe,
}

var relayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Start a signaling relay server",
	Long: `Start a WebSocket relay server for SDP exchange.

This allows terminal-tunnel hosts and clients on different networks
to exchange connection information without direct connectivity.

The relay only handles SDP signaling (~2KB per connection).
All terminal traffic goes directly peer-to-peer after connection.

Example:
  terminal-tunnel relay --port 8765

Clients connect using:
  terminal-tunnel serve --password secret --relay ws://your-relay:8765`,
	RunE: runRelay,
}

var relayPort int

var (
	password string
	shell    string
	timeout  time.Duration
	relayURL string
	noRelay  bool
	manual   bool
)

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(relayCmd)

	// Serve command flags
	serveCmd.Flags().StringVarP(&password, "password", "p", "", "Session password (auto-generated if not provided)")
	serveCmd.Flags().StringVarP(&shell, "shell", "s", "", "Shell to run (default: $SHELL or /bin/sh)")
	serveCmd.Flags().DurationVarP(&timeout, "timeout", "t", 5*time.Minute, "Timeout waiting for client connection")
	serveCmd.Flags().StringVar(&relayURL, "relay", "", "WebSocket relay URL for signaling (e.g., ws://relay.example.com:8765)")
	serveCmd.Flags().BoolVar(&noRelay, "no-relay", false, "Disable relay signaling, use manual mode if UPnP fails")
	serveCmd.Flags().BoolVar(&manual, "manual", false, "Force manual (QR/copy-paste) signaling mode")

	// Relay command flags
	relayCmd.Flags().IntVar(&relayPort, "port", 8765, "Port to listen on for WebSocket connections")
}

func runServe(cmd *cobra.Command, args []string) error {
	// Generate password if not provided
	if password == "" {
		password = generatePassword()
		fmt.Printf("Generated password: %s\n", password)
	}

	opts := server.Options{
		Password: password,
		Shell:    shell,
		Timeout:  timeout,
		RelayURL: relayURL,
		NoRelay:  noRelay,
		Manual:   manual,
	}

	srv, err := server.NewServer(opts)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	return srv.Start()
}

// generatePassword creates a random 16-character password
func generatePassword() string {
	b := make([]byte, 12)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func runRelay(cmd *cobra.Command, args []string) error {
	fmt.Printf("Starting relay server on port %d...\n", relayPort)
	fmt.Printf("\n")
	fmt.Printf("Hosts can connect using:\n")
	fmt.Printf("  terminal-tunnel serve -p <password> --relay ws://<your-ip>:%d\n", relayPort)
	fmt.Printf("\n")

	rs := relayserver.NewRelayServer()
	return rs.Start(relayPort)
}
