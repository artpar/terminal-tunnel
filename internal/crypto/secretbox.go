package crypto

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/nacl/secretbox"
)

const (
	nonceLen = 24 // NaCl nonce size
)

var (
	ErrDecryptionFailed = errors.New("decryption failed: invalid ciphertext or key")
	ErrCiphertextShort  = errors.New("ciphertext too short")
)

// Encrypt encrypts plaintext using NaCl SecretBox with a random nonce.
// Returns: nonce (24 bytes) || ciphertext (with 16-byte auth tag)
func Encrypt(plaintext []byte, key *[32]byte) ([]byte, error) {
	var nonce [nonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}

	// secretbox.Seal appends the encrypted data to the first argument
	// Format: nonce || ciphertext
	encrypted := make([]byte, nonceLen)
	copy(encrypted, nonce[:])
	encrypted = secretbox.Seal(encrypted, plaintext, &nonce, key)

	return encrypted, nil
}

// Decrypt decrypts ciphertext encrypted with Encrypt.
// Expects: nonce (24 bytes) || ciphertext (with 16-byte auth tag)
func Decrypt(ciphertext []byte, key *[32]byte) ([]byte, error) {
	if len(ciphertext) < nonceLen+secretbox.Overhead {
		return nil, ErrCiphertextShort
	}

	var nonce [nonceLen]byte
	copy(nonce[:], ciphertext[:nonceLen])

	plaintext, ok := secretbox.Open(nil, ciphertext[nonceLen:], &nonce, key)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// EncryptedDataChannel wraps a data channel with encryption.
type EncryptedDataChannel struct {
	key       *[32]byte
	sendFunc  func([]byte) error
	onMessage func([]byte)
}

// NewEncryptedDataChannel creates an encrypted wrapper for a data channel.
func NewEncryptedDataChannel(key *[32]byte, sendFunc func([]byte) error) *EncryptedDataChannel {
	return &EncryptedDataChannel{
		key:      key,
		sendFunc: sendFunc,
	}
}

// Send encrypts and sends data.
func (e *EncryptedDataChannel) Send(data []byte) error {
	encrypted, err := Encrypt(data, e.key)
	if err != nil {
		return err
	}
	return e.sendFunc(encrypted)
}

// OnMessage sets the handler for decrypted incoming messages.
func (e *EncryptedDataChannel) OnMessage(handler func([]byte)) {
	e.onMessage = handler
}

// HandleMessage decrypts an incoming message and calls the handler.
func (e *EncryptedDataChannel) HandleMessage(ciphertext []byte) error {
	plaintext, err := Decrypt(ciphertext, e.key)
	if err != nil {
		return err
	}
	if e.onMessage != nil {
		e.onMessage(plaintext)
	}
	return nil
}
