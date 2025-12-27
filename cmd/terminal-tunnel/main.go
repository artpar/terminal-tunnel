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
	"github.com/artpar/terminal-tunnel/internal/recording"
	"github.com/artpar/terminal-tunnel/internal/signaling/relayserver"
)

// setSysProcAttr is defined in daemon_unix.go and daemon_windows.go

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

// Recording commands
var playCmd = &cobra.Command{
	Use:   "play <file>",
	Short: "Play back a recorded session",
	Long: `Play back a previously recorded terminal session.

Recordings are stored in ~/.tt/recordings/ in asciicast v2 format
and can be played with this command or with asciinema.

Example:
  tt play ~/.tt/recordings/2024-01-01_12-00-00_ABC123.cast
  tt play --speed 2 recording.cast`,
	Args: cobra.ExactArgs(1),
	RunE: runPlay,
}

var recordingsCmd = &cobra.Command{
	Use:   "recordings",
	Short: "List recorded sessions",
	Long:  `List all recorded terminal sessions in ~/.tt/recordings/`,
	RunE:  runRecordings,
}

var (
	// Session start flags
	password string
	shell    string
	noTURN   bool
	public   bool
	record   bool

	// Relay flags
	relayPort int

	// Play flags
	playSpeed float64
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

	// Recording commands
	rootCmd.AddCommand(playCmd)
	rootCmd.AddCommand(recordingsCmd)

	// Start command flags
	startCmd.Flags().StringVarP(&password, "password", "p", "", "Session password (auto-generated if not provided)")
	startCmd.Flags().StringVarP(&shell, "shell", "s", "", "Shell to run (default: $SHELL or /bin/sh)")
	startCmd.Flags().BoolVar(&noTURN, "no-turn", false, "Disable TURN relay (P2P only, may fail with symmetric NAT)")
	startCmd.Flags().BoolVar(&public, "public", false, "Enable public viewer mode (read-only viewers without password)")
	startCmd.Flags().BoolVar(&record, "record", false, "Record session to ~/.tt/recordings/")

	// Relay command flags
	relayCmd.Flags().IntVar(&relayPort, "port", 8765, "Port to listen on for WebSocket connections")

	// Play command flags
	playCmd.Flags().Float64Var(&playSpeed, "speed", 1.0, "Playback speed (e.g., 2.0 for 2x speed)")
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

	// Detach from parent process (platform-specific)
	setSysProcAttr(daemonCmd)

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

	result, err := c.StartSession(password, shell, noTURN, public, record)
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}

	fmt.Printf("\nSession started:\n")
	fmt.Printf("  ID:         %s\n", result.ID)
	if result.ShortCode != "" {
		fmt.Printf("  Code:       %s\n", result.ShortCode)
	}
	fmt.Printf("  Password:   %s\n", result.Password)
	if result.ClientURL != "" {
		fmt.Printf("  Control URL: %s\n", result.ClientURL)
	}
	fmt.Printf("  Status:     %s\n", result.Status)

	// Display viewer info if public mode
	if result.Public && result.ViewerCode != "" {
		fmt.Printf("\n  Viewer Code: %s (read-only, no password)\n", result.ViewerCode)
		if result.ViewerURL != "" {
			fmt.Printf("  Viewer URL:  %s\n", result.ViewerURL)
		}
	}
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

func runPlay(cmd *cobra.Command, args []string) error {
	path := args[0]

	// Load recording
	rec, err := recording.LoadRecording(path)
	if err != nil {
		return fmt.Errorf("failed to load recording: %w", err)
	}

	fmt.Printf("Playing: %s\n", path)
	fmt.Printf("Size: %dx%d, Duration: %v, Events: %d\n",
		rec.Header.Width, rec.Header.Height,
		rec.Duration().Round(time.Second), rec.EventCount())
	fmt.Printf("Speed: %.1fx\n\n", playSpeed)
	fmt.Printf("Press Ctrl+C to stop playback\n\n")

	// Set up signal handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create player
	player := recording.NewPlayer(rec, os.Stdout)
	player.SetSpeed(playSpeed)

	// Play in goroutine so we can handle signals
	done := make(chan error, 1)
	go func() {
		done <- player.Play()
	}()

	// Wait for completion or signal
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-sigCh:
		player.Stop()
		fmt.Printf("\n\nPlayback stopped\n")
	}

	fmt.Printf("\n\nPlayback complete\n")
	return nil
}

func runRecordings(cmd *cobra.Command, args []string) error {
	recordings, err := recording.ListRecordings()
	if err != nil {
		return fmt.Errorf("failed to list recordings: %w", err)
	}

	if len(recordings) == 0 {
		fmt.Printf("No recordings found in %s\n", recording.GetRecordingsDir())
		fmt.Println("\nRecord a session with: tt start --record")
		return nil
	}

	fmt.Printf("Recordings in %s:\n\n", recording.GetRecordingsDir())

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE\tCREATED")
	for _, r := range recordings {
		size := formatSize(r.Size)
		age := formatAge(time.Since(r.ModTime))
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, size, age)
	}
	w.Flush()

	fmt.Printf("\nPlay with: tt play <file>\n")
	return nil
}

// formatSize formats a byte count as human-readable
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

