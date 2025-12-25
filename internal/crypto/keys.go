// Package crypto provides encryption primitives for terminal-tunnel
package crypto

import (
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

const (
	// Argon2id parameters - balanced for security and performance
	argonTime    = 3         // Number of iterations
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4         // Parallelism
	argonKeyLen  = 32        // 256-bit key
	saltLen      = 16        // 128-bit salt
)

// DeriveKey derives a 256-bit encryption key from a password using Argon2id.
// The salt should be randomly generated and shared with the peer.
func DeriveKey(password string, salt []byte) [32]byte {
	key := argon2.IDKey(
		[]byte(password),
		salt,
		argonTime,
		argonMemory,
		argonThreads,
		argonKeyLen,
	)

	var keyArray [32]byte
	copy(keyArray[:], key)
	return keyArray
}

// GenerateSalt creates a cryptographically secure random salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}
