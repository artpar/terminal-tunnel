package server

import (
	"sync"
)

// RingBuffer is a fixed-size circular buffer for storing PTY output during disconnection
type RingBuffer struct {
	data     []byte
	size     int
	writePos int
	readPos  int
	count    int
	mu       sync.Mutex
}

// NewRingBuffer creates a new ring buffer with the specified size
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]byte, size),
		size: size,
	}
}

// Write adds data to the buffer, overwriting oldest data if full
func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for _, b := range p {
		rb.data[rb.writePos] = b
		rb.writePos = (rb.writePos + 1) % rb.size

		if rb.count < rb.size {
			rb.count++
		} else {
			// Buffer is full, advance read position (drop oldest)
			rb.readPos = (rb.readPos + 1) % rb.size
		}
	}

	return len(p), nil
}

// ReadAll returns all buffered data and clears the buffer
func (rb *RingBuffer) ReadAll() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil
	}

	result := make([]byte, rb.count)
	for i := 0; i < rb.count; i++ {
		result[i] = rb.data[(rb.readPos+i)%rb.size]
	}

	// Clear buffer
	rb.readPos = 0
	rb.writePos = 0
	rb.count = 0

	return result
}

// Len returns the number of bytes currently in the buffer
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// Clear empties the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.readPos = 0
	rb.writePos = 0
	rb.count = 0
}

// DefaultBufferSize is 64KB - enough for several screens of terminal output
const DefaultBufferSize = 64 * 1024

// BufferedBridge extends Bridge with output buffering for reconnection support
type BufferedBridge struct {
	*Bridge
	buffer       *RingBuffer
	buffering    bool
	bufferingMu  sync.Mutex
}

// NewBufferedBridge creates a bridge with output buffering capability
func NewBufferedBridge(pty *PTY, send func([]byte) error) *BufferedBridge {
	bb := &BufferedBridge{
		Bridge: NewBridge(pty, send),
		buffer: NewRingBuffer(DefaultBufferSize),
	}
	return bb
}

// StartBuffering enables buffering mode (call when client disconnects)
func (bb *BufferedBridge) StartBuffering() {
	bb.bufferingMu.Lock()
	defer bb.bufferingMu.Unlock()
	bb.buffering = true
}

// StopBuffering disables buffering mode and returns buffered data
func (bb *BufferedBridge) StopBuffering() []byte {
	bb.bufferingMu.Lock()
	defer bb.bufferingMu.Unlock()
	bb.buffering = false
	return bb.buffer.ReadAll()
}

// IsBuffering returns true if currently in buffering mode
func (bb *BufferedBridge) IsBuffering() bool {
	bb.bufferingMu.Lock()
	defer bb.bufferingMu.Unlock()
	return bb.buffering
}

// BufferedLen returns the amount of data currently buffered
func (bb *BufferedBridge) BufferedLen() int {
	return bb.buffer.Len()
}
