package protocol

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"sync"
)

// Compression threshold - only compress if data is larger than this
const CompressionThreshold = 128

// Compressor pool to reduce allocations
var compressorPool = sync.Pool{
	New: func() interface{} {
		w, _ := flate.NewWriter(nil, flate.BestSpeed)
		return w
	},
}

// MaxDecompressedSize limits decompression to prevent zip bombs (10MB)
const MaxDecompressedSize = 10 * 1024 * 1024

// Compress compresses data using DEFLATE if it's above threshold
// Returns original data if compression doesn't reduce size
func Compress(data []byte) ([]byte, bool) {
	if len(data) < CompressionThreshold {
		return data, false
	}

	var buf bytes.Buffer
	w := compressorPool.Get().(*flate.Writer)
	w.Reset(&buf)

	if _, err := w.Write(data); err != nil {
		compressorPool.Put(w)
		return data, false
	}

	if err := w.Close(); err != nil {
		compressorPool.Put(w)
		return data, false
	}

	compressorPool.Put(w)

	// Only use compressed if it's actually smaller
	if buf.Len() < len(data) {
		return buf.Bytes(), true
	}

	return data, false
}

// Decompress decompresses DEFLATE-compressed data with size limit to prevent zip bombs
func Decompress(data []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer func() { _ = r.Close() }()

	// Limit decompression size to prevent zip bombs
	limitedReader := io.LimitReader(r, MaxDecompressedSize+1)
	var buf bytes.Buffer
	n, err := io.Copy(&buf, limitedReader)
	if err != nil {
		return nil, err
	}
	if n > MaxDecompressedSize {
		return nil, fmt.Errorf("decompressed data exceeds maximum size of %d bytes", MaxDecompressedSize)
	}

	return buf.Bytes(), nil
}

// CompressedMessage adds compression support to protocol messages
type CompressedMessage struct {
	Type       MsgType
	Compressed bool
	Payload    []byte
}

// Additional message types for compressed data
const (
	MsgDataCompressed MsgType = 0x10 // Compressed terminal data
)

// NewCompressedDataMessage creates a data message with optional compression
func NewCompressedDataMessage(data []byte) *Message {
	compressed, isCompressed := Compress(data)

	msgType := MsgData
	if isCompressed {
		msgType = MsgDataCompressed
	}

	return &Message{
		Type:    msgType,
		Payload: compressed,
	}
}

// DecompressIfNeeded decompresses payload if message type indicates compression
func DecompressIfNeeded(msg *Message) ([]byte, error) {
	if msg.Type == MsgDataCompressed {
		return Decompress(msg.Payload)
	}
	return msg.Payload, nil
}
