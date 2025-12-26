package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents log severity
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging
type Logger struct {
	output    io.Writer
	level     Level
	component string
	mu        sync.Mutex
	json      bool
}

// Entry represents a log entry
type Entry struct {
	Time      string            `json:"time"`
	Level     string            `json:"level"`
	Component string            `json:"component,omitempty"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

var defaultLogger = &Logger{
	output:    os.Stderr,
	level:     LevelInfo,
	component: "tt",
}

// SetLevel sets the minimum log level
func SetLevel(level Level) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

// SetOutput sets the output writer
func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.output = w
}

// SetJSON enables JSON output
func SetJSON(enabled bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.json = enabled
}

// WithComponent creates a logger with a specific component name
func WithComponent(name string) *Logger {
	return &Logger{
		output:    defaultLogger.output,
		level:     defaultLogger.level,
		component: name,
		json:      defaultLogger.json,
	}
}

func (l *Logger) log(level Level, msg string, fields map[string]string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	if l.json {
		entry := Entry{
			Time:      time.Now().Format(time.RFC3339),
			Level:     level.String(),
			Component: l.component,
			Message:   msg,
			Fields:    fields,
		}
		data, _ := json.Marshal(entry)
		fmt.Fprintln(l.output, string(data))
	} else {
		timestamp := time.Now().Format("15:04:05")
		if len(fields) > 0 {
			fmt.Fprintf(l.output, "[%s] %s [%s] %s %v\n",
				timestamp, level.String(), l.component, msg, fields)
		} else {
			fmt.Fprintf(l.output, "[%s] %s [%s] %s\n",
				timestamp, level.String(), l.component, msg)
		}
	}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields ...map[string]string) {
	var f map[string]string
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(LevelDebug, msg, f)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields ...map[string]string) {
	var f map[string]string
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(LevelInfo, msg, f)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields ...map[string]string) {
	var f map[string]string
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(LevelWarn, msg, f)
}

// Error logs an error message
func (l *Logger) Error(msg string, fields ...map[string]string) {
	var f map[string]string
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(LevelError, msg, f)
}

// Package-level convenience functions
func Debug(msg string, fields ...map[string]string) { defaultLogger.Debug(msg, fields...) }
func Info(msg string, fields ...map[string]string)  { defaultLogger.Info(msg, fields...) }
func Warn(msg string, fields ...map[string]string)  { defaultLogger.Warn(msg, fields...) }
func Error(msg string, fields ...map[string]string) { defaultLogger.Error(msg, fields...) }

// F creates a fields map for logging
func F(keyvals ...string) map[string]string {
	m := make(map[string]string)
	for i := 0; i < len(keyvals)-1; i += 2 {
		m[keyvals[i]] = keyvals[i+1]
	}
	return m
}
