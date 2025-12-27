package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Recorder writes terminal output to an asciicast v2 file
type Recorder struct {
	file      *os.File
	startTime time.Time
	width     int
	height    int
	mu        sync.Mutex
	closed    bool
}

// NewRecorder creates a new recorder that writes to the specified path
func NewRecorder(path string, width, height int, title string) (*Recorder, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create recording directory: %w", err)
	}

	// Create file
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create recording file: %w", err)
	}

	// Set secure permissions
	if err := os.Chmod(path, 0600); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to set file permissions: %w", err)
	}

	r := &Recorder{
		file:      file,
		startTime: time.Now(),
		width:     width,
		height:    height,
	}

	// Write header
	header := DefaultHeader(width, height, title)
	headerData, err := json.Marshal(header)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to marshal header: %w", err)
	}

	if _, err := file.Write(append(headerData, '\n')); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	return r, nil
}

// WriteOutput records terminal output data
func (r *Recorder) WriteOutput(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("recorder is closed")
	}

	elapsed := time.Since(r.startTime).Seconds()

	event := Event{
		Time: elapsed,
		Type: "o", // output
		Data: string(data),
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := r.file.Write(append(eventData, '\n')); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	return nil
}

// WriteInput records terminal input data (optional, for full recording)
func (r *Recorder) WriteInput(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("recorder is closed")
	}

	elapsed := time.Since(r.startTime).Seconds()

	event := Event{
		Time: elapsed,
		Type: "i", // input
		Data: string(data),
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := r.file.Write(append(eventData, '\n')); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	return nil
}

// WriteResize records a terminal resize event
func (r *Recorder) WriteResize(width, height int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("recorder is closed")
	}

	elapsed := time.Since(r.startTime).Seconds()
	r.width = width
	r.height = height

	// Resize data format: "WIDTHxHEIGHT"
	event := Event{
		Time: elapsed,
		Type: "r", // resize
		Data: fmt.Sprintf("%dx%d", width, height),
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := r.file.Write(append(eventData, '\n')); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	return nil
}

// Close closes the recorder and flushes any pending data
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true

	if err := r.file.Sync(); err != nil {
		r.file.Close()
		return fmt.Errorf("failed to sync file: %w", err)
	}

	return r.file.Close()
}

// Path returns the path to the recording file
func (r *Recorder) Path() string {
	return r.file.Name()
}

// Duration returns the current duration of the recording
func (r *Recorder) Duration() time.Duration {
	return time.Since(r.startTime)
}

// GetRecordingsDir returns the default recordings directory
func GetRecordingsDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".tt/recordings"
	}
	return filepath.Join(homeDir, ".tt", "recordings")
}

// GenerateRecordingPath generates a unique recording file path
func GenerateRecordingPath(shortCode string) string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("%s_%s.cast", timestamp, shortCode)
	return filepath.Join(GetRecordingsDir(), filename)
}

// ListRecordings returns all recording files in the recordings directory
func ListRecordings() ([]RecordingInfo, error) {
	dir := GetRecordingsDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read recordings directory: %w", err)
	}

	var recordings []RecordingInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".cast" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		rec, err := LoadRecordingInfo(path)
		if err != nil {
			// Still include file even if we can't parse it
			recordings = append(recordings, RecordingInfo{
				Path:     path,
				Name:     entry.Name(),
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				Duration: 0,
			})
			continue
		}

		recordings = append(recordings, *rec)
	}

	return recordings, nil
}

// RecordingInfo contains metadata about a recording file
type RecordingInfo struct {
	Path     string
	Name     string
	Size     int64
	ModTime  time.Time
	Duration time.Duration
	Width    int
	Height   int
	Title    string
}

// LoadRecordingInfo loads metadata from a recording file
func LoadRecordingInfo(path string) (*RecordingInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// Read just the header line
	buf := make([]byte, 4096)
	n, err := file.Read(buf)
	if err != nil {
		return nil, err
	}

	// Find first newline
	var headerEnd int
	for i := 0; i < n; i++ {
		if buf[i] == '\n' {
			headerEnd = i
			break
		}
	}

	if headerEnd == 0 {
		return nil, fmt.Errorf("no header found")
	}

	var header Header
	if err := json.Unmarshal(buf[:headerEnd], &header); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	// For duration, we'd need to read the last event
	// For now, estimate from file size
	rec := &RecordingInfo{
		Path:    path,
		Name:    filepath.Base(path),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Width:   header.Width,
		Height:  header.Height,
		Title:   header.Title,
	}

	return rec, nil
}
