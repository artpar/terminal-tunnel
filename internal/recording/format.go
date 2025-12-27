// Package recording provides session recording and playback in asciicast v2 format
package recording

import (
	"encoding/json"
	"fmt"
	"time"
)

// Header represents the asciicast v2 header
type Header struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Title     string            `json:"title,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Theme     *Theme            `json:"theme,omitempty"`
}

// Theme represents terminal theme colors
type Theme struct {
	Foreground string `json:"fg,omitempty"`
	Background string `json:"bg,omitempty"`
}

// Event represents a single asciicast event
// Format: [time, event_type, data]
// event_type: "o" for output, "i" for input, "r" for resize
type Event struct {
	Time float64 // Seconds since start
	Type string  // "o" = output, "i" = input, "r" = resize
	Data string  // Event data (terminal output or input)
}

// MarshalJSON implements custom JSON marshaling for Event
// asciicast v2 format: [time, type, data]
func (e Event) MarshalJSON() ([]byte, error) {
	return json.Marshal([]interface{}{e.Time, e.Type, e.Data})
}

// UnmarshalJSON implements custom JSON unmarshaling for Event
func (e *Event) UnmarshalJSON(data []byte) error {
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	if len(arr) != 3 {
		return fmt.Errorf("invalid event format: expected 3 elements, got %d", len(arr))
	}

	// Parse time
	switch v := arr[0].(type) {
	case float64:
		e.Time = v
	case int:
		e.Time = float64(v)
	default:
		return fmt.Errorf("invalid time type: %T", arr[0])
	}

	// Parse type
	var ok bool
	e.Type, ok = arr[1].(string)
	if !ok {
		return fmt.Errorf("invalid type: expected string, got %T", arr[1])
	}

	// Parse data
	e.Data, ok = arr[2].(string)
	if !ok {
		return fmt.Errorf("invalid data: expected string, got %T", arr[2])
	}

	return nil
}

// Recording represents a complete asciicast recording
type Recording struct {
	Header Header
	Events []Event
}

// Duration returns the total duration of the recording
func (r *Recording) Duration() time.Duration {
	if len(r.Events) == 0 {
		return 0
	}
	lastEvent := r.Events[len(r.Events)-1]
	return time.Duration(lastEvent.Time * float64(time.Second))
}

// EventCount returns the number of events in the recording
func (r *Recording) EventCount() int {
	return len(r.Events)
}

// DefaultHeader creates a header with default values
func DefaultHeader(width, height int, title string) Header {
	return Header{
		Version:   2,
		Width:     width,
		Height:    height,
		Timestamp: time.Now().Unix(),
		Title:     title,
		Env: map[string]string{
			"TERM":  "xterm-256color",
			"SHELL": "/bin/sh",
		},
	}
}
