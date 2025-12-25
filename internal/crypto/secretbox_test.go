package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := DeriveKey("test-password", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	plaintext := []byte("Hello, Terminal Tunnel!")

	ciphertext, err := Encrypt(plaintext, &key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Ciphertext should be longer than plaintext (nonce + overhead)
	if len(ciphertext) <= len(plaintext) {
		t.Error("ciphertext should be longer than plaintext")
	}

	decrypted, err := Decrypt(ciphertext, &key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted text doesn't match: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptProducesUniqueCiphertext(t *testing.T) {
	key := DeriveKey("test", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	plaintext := []byte("same message")

	ct1, _ := Encrypt(plaintext, &key)
	ct2, _ := Encrypt(plaintext, &key)

	// Different nonces should produce different ciphertexts
	if bytes.Equal(ct1, ct2) {
		t.Error("encrypting same plaintext twice should produce different ciphertexts")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1 := DeriveKey("password1", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	key2 := DeriveKey("password2", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	ciphertext, _ := Encrypt([]byte("secret"), &key1)

	_, err := Decrypt(ciphertext, &key2)
	if err != ErrDecryptionFailed {
		t.Errorf("expected ErrDecryptionFailed, got %v", err)
	}
}

func TestDecryptTooShort(t *testing.T) {
	key := DeriveKey("test", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	_, err := Decrypt([]byte{1, 2, 3}, &key)
	if err != ErrCiphertextShort {
		t.Errorf("expected ErrCiphertextShort, got %v", err)
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key := DeriveKey("test", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	ciphertext, _ := Encrypt([]byte("secret message"), &key)

	// Tamper with the ciphertext
	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err := Decrypt(ciphertext, &key)
	if err != ErrDecryptionFailed {
		t.Errorf("expected ErrDecryptionFailed for tampered ciphertext, got %v", err)
	}
}

func TestEncryptedDataChannel(t *testing.T) {
	key := DeriveKey("channel-key", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	var sentData []byte
	edc := NewEncryptedDataChannel(&key, func(data []byte) error {
		sentData = data
		return nil
	})

	var receivedData []byte
	edc.OnMessage(func(data []byte) {
		receivedData = data
	})

	// Send a message
	originalMsg := []byte("terminal data")
	err := edc.Send(originalMsg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Simulate receiving the encrypted message
	err = edc.HandleMessage(sentData)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if !bytes.Equal(originalMsg, receivedData) {
		t.Errorf("received data doesn't match: got %q, want %q", receivedData, originalMsg)
	}
}

func TestEncryptEmptyMessage(t *testing.T) {
	key := DeriveKey("test", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	ciphertext, err := Encrypt([]byte{}, &key)
	if err != nil {
		t.Fatalf("Encrypt empty failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, &key)
	if err != nil {
		t.Fatalf("Decrypt empty failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("expected empty decrypted, got %v", decrypted)
	}
}
