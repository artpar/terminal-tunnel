package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/artpar/terminal-tunnel/internal/client"
	"github.com/artpar/terminal-tunnel/internal/daemon"
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
	Use:   "tt",
	Short: "P2P terminal sharing with E2E encryption",
	Long: `Terminal Tunnel (tt) enables secure, peer-to-peer terminal sharing.

Run a terminal session that can be accessed from any device (including Android)
via a web browser. All traffic is end-to-end encrypted with a password you choose.

Example:
  tt daemon start      # Start the daemon
  tt start -p mysecret # Start a session
  tt list              # List all sessions
  tt stop <code>       # Stop a session
  tt daemon stop       # Stop the daemon`,
	Version: version,
}

// Daemon commands
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the terminal-tunnel daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon in background",
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	RunE:  runDaemonStop,
}

var daemonForegroundCmd = &cobra.Command{
	Use:    "foreground",
	Short:  "Run daemon in foreground (internal use)",
	Hidden: true,
	RunE:   runDaemonForeground,
}

// Session commands
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a new terminal session",
	Long: `Start a new terminal sharing session.

The daemon will:
1. Create a PTY with your shell
2. Register with the signaling relay
3. Display a URL and short code for clients
4. Wait for a client to connect
5. Bridge the terminal to the encrypted data channel`,
	RunE: runStart,
}

var stopCmd = &cobra.Command{
	Use:   "stop <id|code>",
	Short: "Stop a terminal session",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all terminal sessions",
	RunE:  runList,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon and session status",
	RunE:  runStatus,
}

// Relay command (kept from original)
var relayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Start a signaling relay server",
	Long: `Start a WebSocket relay server for SDP exchange.

This allows terminal-tunnel hosts and clients on different networks
to exchange connection information without direct connectivity.

The relay only handles SDP signaling (~2KB per connection).
All terminal traffic goes directly peer-to-peer after connection.

Example:
  tt relay --port 8765`,
	RunE: runRelay,
}

var (
	// Session start flags
	password string
	shell    string

	// Relay flags
	relayPort int
)

func init() {
	// Daemon commands
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonForegroundCmd)

	// Session commands
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)

	// Relay command
	rootCmd.AddCommand(relayCmd)

	// Start command flags
	startCmd.Flags().StringVarP(&password, "password", "p", "", "Session password (auto-generated if not provided)")
	startCmd.Flags().StringVarP(&shell, "shell", "s", "", "Shell to run (default: $SHELL or /bin/sh)")

	// Relay command flags
	relayCmd.Flags().IntVar(&relayPort, "port", 8765, "Port to listen on for WebSocket connections")
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	// Check if already running
	running, pid := daemon.IsDaemonRunning()
	if running {
		fmt.Printf("Daemon already running (PID %d)\n", pid)
		return nil
	}

	// Start daemon in background
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	daemonCmd := exec.Command(executable, "daemon", "foreground")
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil
	daemonCmd.Stdin = nil

	// Detach from parent process
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait briefly for daemon to start
	time.Sleep(500 * time.Millisecond)

	// Verify it started
	running, pid = daemon.IsDaemonRunning()
	if !running {
		return fmt.Errorf("daemon failed to start")
	}

	fmt.Printf("Daemon started (PID %d)\n", pid)
	return nil
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	c := client.NewClient()

	if !c.IsDaemonRunning() {
		fmt.Println("Daemon is not running")
		return nil
	}

	result, err := c.Shutdown()
	if err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	fmt.Printf("Daemon stopped (%d sessions terminated)\n", result.SessionsStopped)
	return nil
}

func runDaemonForeground(cmd *cobra.Command, args []string) error {
	// This runs the daemon in the foreground (used when backgrounding)
	d, err := daemon.NewDaemon()
	if err != nil {
		return err
	}

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		d.Shutdown()
	}()

	return d.Start()
}

func runStart(cmd *cobra.Command, args []string) error {
	c := client.NewClient()

	// Check if daemon is running
	if !c.IsDaemonRunning() {
		fmt.Println("Daemon is not running. Start it with: tt daemon start")
		return nil
	}

	result, err := c.StartSession(password, shell)
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}

	fmt.Printf("\nSession started:\n")
	fmt.Printf("  ID:       %s\n", result.ID)
	if result.ShortCode != "" {
		fmt.Printf("  Code:     %s\n", result.ShortCode)
	}
	fmt.Printf("  Password: %s\n", result.Password)
	if result.ClientURL != "" {
		fmt.Printf("  URL:      %s\n", result.ClientURL)
	}
	fmt.Printf("  Status:   %s\n", result.Status)
	fmt.Println()

	return nil
}

func runStop(cmd *cobra.Command, args []string) error {
	c := client.NewClient()

	if !c.IsDaemonRunning() {
		fmt.Println("Daemon is not running")
		return nil
	}

	idOrCode := args[0]
	if err := c.StopSession(idOrCode); err != nil {
		return fmt.Errorf("failed to stop session: %w", err)
	}

	fmt.Printf("Session %s stopped\n", idOrCode)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	c := client.NewClient()

	if !c.IsDaemonRunning() {
		fmt.Println("Daemon is not running")
		return nil
	}

	sessions, err := c.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCODE\tSTATUS\tSHELL\tCREATED")
	for _, s := range sessions {
		age := formatAge(time.Since(s.CreatedAt))
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.ID, s.ShortCode, s.Status, s.Shell, age)
	}
	w.Flush()

	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	c := client.NewClient()

	if !c.IsDaemonRunning() {
		fmt.Println("Daemon: not running")
		fmt.Println("\nStart with: tt daemon start")
		return nil
	}

	status, err := c.Status()
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	fmt.Printf("Daemon: running (PID %d, uptime %s)\n", status.PID, status.Uptime)
	fmt.Printf("Sessions: %d total", status.SessionCount)
	if status.ActiveCount > 0 {
		fmt.Printf(", %d connected", status.ActiveCount)
	}
	fmt.Println()

	return nil
}

func runRelay(cmd *cobra.Command, args []string) error {
	fmt.Printf("Starting relay server on port %d...\n", relayPort)
	fmt.Printf("\n")
	fmt.Printf("Hosts can use this relay with:\n")
	fmt.Printf("  Set RELAY_URL=ws://<your-ip>:%d in environment\n", relayPort)
	fmt.Printf("\n")

	rs := relayserver.NewRelayServer()
	return rs.Start(relayPort)
}

// formatAge formats a duration as a human-readable age
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

