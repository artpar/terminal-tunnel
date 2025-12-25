package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateSalt(t *testing.T) {
	salt1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt failed: %v", err)
	}

	if len(salt1) != saltLen {
		t.Errorf("expected salt length %d, got %d", saltLen, len(salt1))
	}

	// Ensure salts are unique
	salt2, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt failed: %v", err)
	}

	if bytes.Equal(salt1, salt2) {
		t.Error("two consecutive salts should not be equal")
	}
}

func TestDeriveKey(t *testing.T) {
	salt, _ := GenerateSalt()
	password := "test-password-123"

	key1 := DeriveKey(password, salt)

	// Key should be 32 bytes
	if len(key1) != 32 {
		t.Errorf("expected key length 32, got %d", len(key1))
	}

	// Same password and salt should produce same key
	key2 := DeriveKey(password, salt)
	if key1 != key2 {
		t.Error("same password and salt should produce same key")
	}

	// Different password should produce different key
	key3 := DeriveKey("different-password", salt)
	if key1 == key3 {
		t.Error("different passwords should produce different keys")
	}

	// Different salt should produce different key
	salt2, _ := GenerateSalt()
	key4 := DeriveKey(password, salt2)
	if key1 == key4 {
		t.Error("different salts should produce different keys")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	// Fixed salt for deterministic test
	salt := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	password := "fixed-password"

	key1 := DeriveKey(password, salt)
	key2 := DeriveKey(password, salt)

	if key1 != key2 {
		t.Error("key derivation should be deterministic")
	}
}
