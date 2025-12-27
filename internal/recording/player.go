package recording

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Player plays back an asciicast recording
type Player struct {
	recording *Recording
	speed     float64
	output    io.Writer
	index     int
	paused    bool
	stopped   bool
}

// NewPlayer creates a new player for the given recording
func NewPlayer(rec *Recording, output io.Writer) *Player {
	return &Player{
		recording: rec,
		speed:     1.0,
		output:    output,
		index:     0,
	}
}

// LoadRecording loads a recording from a file
func LoadRecording(path string) (*Recording, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open recording: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large lines
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	rec := &Recording{}

	// First line is the header
	if !scanner.Scan() {
		return nil, fmt.Errorf("empty recording file")
	}

	if err := json.Unmarshal(scanner.Bytes(), &rec.Header); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	// Rest are events
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			// Skip malformed lines
			continue
		}
		rec.Events = append(rec.Events, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read recording: %w", err)
	}

	return rec, nil
}

// SetSpeed sets the playback speed (1.0 = normal, 2.0 = 2x, 0.5 = half speed)
func (p *Player) SetSpeed(speed float64) {
	if speed <= 0 {
		speed = 1.0
	}
	p.speed = speed
}

// Play plays the recording from the beginning
func (p *Player) Play() error {
	p.index = 0
	p.stopped = false
	return p.Resume()
}

// Resume continues playback from current position
func (p *Player) Resume() error {
	p.paused = false

	var lastTime float64 = 0
	if p.index > 0 && p.index <= len(p.recording.Events) {
		lastTime = p.recording.Events[p.index-1].Time
	}

	for p.index < len(p.recording.Events) {
		if p.stopped {
			return nil
		}
		if p.paused {
			return nil
		}

		event := p.recording.Events[p.index]

		// Calculate delay
		delay := event.Time - lastTime
		if delay > 0 {
			// Apply speed adjustment
			adjustedDelay := time.Duration(float64(time.Second) * delay / p.speed)

			// Cap maximum delay to 2 seconds
			if adjustedDelay > 2*time.Second {
				adjustedDelay = 2 * time.Second
			}

			time.Sleep(adjustedDelay)
		}

		// Handle event based on type
		switch event.Type {
		case "o": // output
			p.output.Write([]byte(event.Data))
		case "i": // input - typically not played back
			// Could optionally display input differently
		case "r": // resize
			// Could signal terminal resize if supported
		}

		lastTime = event.Time
		p.index++
	}

	return nil
}

// Pause pauses playback
func (p *Player) Pause() {
	p.paused = true
}

// Stop stops playback
func (p *Player) Stop() {
	p.stopped = true
}

// IsPaused returns whether playback is paused
func (p *Player) IsPaused() bool {
	return p.paused
}

// IsPlaying returns whether playback is active
func (p *Player) IsPlaying() bool {
	return !p.paused && !p.stopped && p.index < len(p.recording.Events)
}

// Progress returns the current playback position (0.0 to 1.0)
func (p *Player) Progress() float64 {
	if len(p.recording.Events) == 0 {
		return 0
	}
	return float64(p.index) / float64(len(p.recording.Events))
}

// CurrentTime returns the current playback time
func (p *Player) CurrentTime() time.Duration {
	if p.index == 0 || len(p.recording.Events) == 0 {
		return 0
	}
	if p.index >= len(p.recording.Events) {
		return p.recording.Duration()
	}
	return time.Duration(p.recording.Events[p.index].Time * float64(time.Second))
}

// Seek seeks to a specific time in the recording
func (p *Player) Seek(t time.Duration) {
	targetTime := t.Seconds()

	// Find the event at or before this time
	p.index = 0
	for i, event := range p.recording.Events {
		if event.Time > targetTime {
			break
		}
		p.index = i + 1
	}
}

// SeekPercent seeks to a percentage of the recording
func (p *Player) SeekPercent(percent float64) {
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}

	duration := p.recording.Duration()
	targetTime := time.Duration(float64(duration) * percent)
	p.Seek(targetTime)
}

// PlayInstant plays up to current position instantly (for seek preview)
func (p *Player) PlayInstant() error {
	for i := 0; i < p.index; i++ {
		event := p.recording.Events[i]
		if event.Type == "o" {
			p.output.Write([]byte(event.Data))
		}
	}
	return nil
}

// GetRecording returns the underlying recording
func (p *Player) GetRecording() *Recording {
	return p.recording
}
