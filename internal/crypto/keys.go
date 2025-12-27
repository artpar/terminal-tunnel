// Package crypto provides encryption primitives for terminal-tunnel
package crypto

import (
	"crypto/rand"
	"crypto/sha256"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

const (
	// Argon2id parameters - balanced for security and performance
	argonTime    = 3         // Number of iterations
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4         // Parallelism
	argonKeyLen  = 32        // 256-bit key
	saltLen      = 16        // 128-bit salt

	// PBKDF2 parameters - fallback for environments where Argon2 WASM is blocked
	pbkdf2Iterations = 600000 // High iteration count for security
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

// DeriveKeyPBKDF2 derives a 256-bit encryption key using PBKDF2-SHA256.
// This is a fallback for browsers where Argon2 WASM is blocked by CSP.
func DeriveKeyPBKDF2(password string, salt []byte) [32]byte {
	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, argonKeyLen, sha256.New)
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

// GenerateRandomKey creates a cryptographically secure random 256-bit key.
// Used for public viewer sessions where key is stored in relay.
func GenerateRandomKey() ([]byte, error) {
	key := make([]byte, argonKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}
