package crypto

import (
	"bytes"
	"testing"
)

// testKey returns a deterministic 32-byte key for testing.
func testKey(t *testing.T) []byte {
	t.Helper()
	salt := []byte("16-byte-salt-val") // 16 bytes exactly
	return DeriveKey("test-password-123", salt)
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("This is a secret message that should be encrypted.")

	// Encrypt
	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Verify encrypted data is at least nonce + ciphertext + auth tag
	minExpectedSize := NonceSize + len(plaintext) + 16 // 16 = GCM auth tag
	if len(encrypted) < minExpectedSize {
		t.Errorf("Encrypted data too short: got %d bytes, expected at least %d", len(encrypted), minExpectedSize)
	}

	// Verify encrypted data is different from plaintext
	if bytes.Contains(encrypted, plaintext) {
		t.Error("Encrypted data contains plaintext (encryption failed)")
	}

	// Decrypt
	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	// Verify decrypted matches original
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decrypted data doesn't match original.\nGot:  %q\nWant: %q", decrypted, plaintext)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	correctKey := testKey(t)
	wrongKey := DeriveKey("wrong-password", []byte("16-byte-salt-val"))
	plaintext := []byte("Secret data")

	// Encrypt with correct key
	encrypted, err := Encrypt(plaintext, correctKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Try to decrypt with wrong key
	_, err = Decrypt(encrypted, wrongKey)
	if err == nil {
		t.Error("Decrypt should fail with wrong key, but succeeded")
	}
}

func TestEncrypt_EmptyKey(t *testing.T) {
	plaintext := []byte("Some data")

	_, err := Encrypt(plaintext, []byte{})
	if err == nil {
		t.Error("Encrypt should fail with empty key, but succeeded")
	}
}

func TestDecrypt_TamperedData(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("Original secret")

	// Encrypt
	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Tamper with the ciphertext (flip a bit in the middle)
	tampered := make([]byte, len(encrypted))
	copy(tampered, encrypted)
	midpoint := len(tampered) / 2
	tampered[midpoint] ^= 0xFF // Flip all bits in one byte

	// Try to decrypt tampered data
	_, err = Decrypt(tampered, key)
	if err == nil {
		t.Error("Decrypt should fail with tampered data (GCM auth should fail), but succeeded")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	key := testKey(t)
	tooShort := []byte("short") // Less than nonce + auth tag

	_, err := Decrypt(tooShort, key)
	if err == nil {
		t.Error("Decrypt should fail with data too short, but succeeded")
	}
}

func TestEncrypt_UniqueNonces(t *testing.T) {
	// Clear session to ensure clean state
	_ = ClearSession()

	key := testKey(t)
	plaintext := []byte("Same plaintext")

	// Encrypt the same data twice
	encrypted1, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("First encrypt failed: %v", err)
	}

	encrypted2, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Second encrypt failed: %v", err)
	}

	// Nonces should be different (first NonceSize bytes)
	nonce1 := encrypted1[:NonceSize]
	nonce2 := encrypted2[:NonceSize]

	if bytes.Equal(nonce1, nonce2) {
		t.Error("Nonces should be unique for each encryption, but they're identical")
	}

	// Ciphertexts should be different
	if bytes.Equal(encrypted1, encrypted2) {
		t.Error("Encrypted outputs should differ (due to unique nonces), but they're identical")
	}

	// Both should decrypt correctly
	decrypted1, err := Decrypt(encrypted1, key)
	if err != nil || !bytes.Equal(decrypted1, plaintext) {
		t.Error("First encrypted data failed to decrypt correctly")
	}

	decrypted2, err := Decrypt(encrypted2, key)
	if err != nil || !bytes.Equal(decrypted2, plaintext) {
		t.Error("Second encrypted data failed to decrypt correctly")
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	_ = ClearSession()

	key := testKey(t)
	plaintext := []byte{}

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt empty plaintext failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt empty plaintext failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decrypted empty plaintext doesn't match.\nGot:  %q\nWant: %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_LargeData(t *testing.T) {
	_ = ClearSession()

	key := testKey(t)
	// Create 1MB of data
	plaintext := make([]byte, 1024*1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt large data failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt large data failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("Decrypted large data doesn't match original")
	}
}

func TestDeriveKey(t *testing.T) {
	password := "test-password"
	salt := []byte("16-byte-salt-val") // 16 bytes

	// Derive key twice with same inputs
	key1 := DeriveKey(password, salt)
	key2 := DeriveKey(password, salt)

	// Should be identical
	if !bytes.Equal(key1, key2) {
		t.Error("DeriveKey should produce identical keys for same inputs")
	}

	// Should be 32 bytes (AES-256)
	if len(key1) != Argon2KeyLength {
		t.Errorf("Derived key wrong length: got %d, want %d", len(key1), Argon2KeyLength)
	}

	// Different salt should produce different key
	differentSalt := []byte("different-salt-!")
	key3 := DeriveKey(password, differentSalt)

	if bytes.Equal(key1, key3) {
		t.Error("DeriveKey should produce different keys for different salts")
	}
}
