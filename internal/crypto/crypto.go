package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/crypto/argon2"

	"github.com/DeprecatedLuar/dredge/internal/session"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

// Debug mode flag (set from main)
var DebugMode bool

// pendingPassword holds a password provided via --password flag, used once by GetKeyWithVerification.
// In-memory only — never written to disk.
var pendingPassword string

// SetPendingPassword stores a password to be used once by GetKeyWithVerification instead of prompting.
// Called from main.go when --password flag is provided.
func SetPendingPassword(pw string) {
	pendingPassword = pw
}

// ============================================================================
// Constants
// ============================================================================

const (
	SaltSize  = 16 // 128 bits — global salt stored in .dredge-key
	NonceSize = 12 // 96 bits (standard GCM nonce size)
	KeySize   = 32 // 256 bits for AES-256

	// Argon2id parameters (per RFC 9106 recommendations)
	Argon2Time      = 1         // 1 iteration
	Argon2Memory    = 64 * 1024 // 64 MB
	Argon2Threads   = 4         // 4 parallel threads
	Argon2KeyLength = 32        // 32 bytes for AES-256
)

// Password verification
const (
	PasswordVerifyFile  = ".dredge-key"
	VerificationContent = "dredge-vault-v1"
)

// ============================================================================
// Core Encryption Functions
// ============================================================================

// Encrypt encrypts plaintext using a pre-derived 32-byte key (AES-256-GCM).
// Returns binary format: [12B nonce][N bytes ciphertext + 16B auth tag]
// Use DeriveKey or GetKeyWithVerification to obtain a key.
func Encrypt(plaintext []byte, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	result := make([]byte, 0, NonceSize+len(ciphertext))
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// Decrypt decrypts data using a pre-derived 32-byte key.
// Input format: [12B nonce][N bytes ciphertext + 16B auth tag]
func Decrypt(encrypted []byte, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeySize, len(key))
	}

	minSize := NonceSize + 16 // 16 = GCM auth tag
	if len(encrypted) < minSize {
		return nil, fmt.Errorf("encrypted data too short: got %d bytes, need at least %d", len(encrypted), minSize)
	}

	nonce := encrypted[:NonceSize]
	ciphertext := encrypted[NonceSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong key or tampered data): %w", err)
	}

	return plaintext, nil
}

// DeriveKey derives an encryption key from password and salt using Argon2id.
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey(
		[]byte(password),
		salt,
		Argon2Time,
		Argon2Memory,
		Argon2Threads,
		Argon2KeyLength,
	)
}

// ExtractSalt returns the first SaltSize bytes from data (global salt from .dredge-key).
func ExtractSalt(data []byte) []byte {
	if len(data) < SaltSize {
		return nil
	}
	return data[:SaltSize]
}

// ============================================================================
// Session Management
// ============================================================================

const sessionCacheFile = ".key" // Raw 32-byte derived master key

// GetCachedKey retrieves the cached 32-byte master key from session.
// Returns nil if cache doesn't exist or is not exactly KeySize bytes.
func GetCachedKey() ([]byte, error) {
	cachePath := filepath.Join(session.Dir(), sessionCacheFile)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read session cache: %w", err)
	}

	if len(data) != KeySize {
		return nil, nil // corrupt cache, treat as missing
	}

	return data, nil
}

// CacheKey stores the 32-byte master key in session.
func CacheKey(key []byte) error {
	if len(key) != KeySize {
		return fmt.Errorf("key must be %d bytes, got %d", KeySize, len(key))
	}

	if err := os.MkdirAll(session.Dir(), 0700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	cachePath := filepath.Join(session.Dir(), sessionCacheFile)
	if err := os.WriteFile(cachePath, key, 0600); err != nil {
		return fmt.Errorf("failed to cache key: %w", err)
	}

	return nil
}

// ClearSession removes the session cache file.
func ClearSession() error {
	cachePath := filepath.Join(session.Dir(), sessionCacheFile)
	err := os.Remove(cachePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear session cache: %w", err)
	}
	return nil
}

// HasActiveSession checks if a valid session key exists for current terminal.
func HasActiveSession() bool {
	key, err := GetCachedKey()
	return err == nil && len(key) == KeySize
}

// GetPPID returns the parent process ID (for debugging/testing).
func GetPPID() string {
	return strconv.Itoa(os.Getppid())
}

// ============================================================================
// Password Verification
// ============================================================================

// GetVerifyFilePath returns the full path to the password verification file.
func GetVerifyFilePath() (string, error) {
	baseDir := os.Getenv("XDG_DATA_HOME")
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".local", "share")
	}

	dredgeDir := filepath.Join(baseDir, "dredge")
	return filepath.Join(dredgeDir, PasswordVerifyFile), nil
}

// PasswordVerificationExists checks if the .dredge-key file exists.
func PasswordVerificationExists() bool {
	path, err := GetVerifyFilePath()
	if err != nil {
		return false
	}

	_, err = os.Stat(path)
	return err == nil
}

// NewVerificationFileBytes generates the bytes for a .dredge-key file and the derived master key.
// File format: [16B salt][12B nonce][ciphertext + auth tag]
// Returns (fileBytes, masterKey, error). Use this when you need both the bytes and the key.
func NewVerificationFileBytes(password string) ([]byte, []byte, error) {
	if password == "" {
		return nil, nil, fmt.Errorf("password cannot be empty")
	}

	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	key := DeriveKey(password, salt)

	encrypted, err := Encrypt([]byte(VerificationContent), key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encrypt verification: %w", err)
	}

	fileBytes := append(salt, encrypted...)
	return fileBytes, key, nil
}

// CreatePasswordVerification creates the .dredge-key file with the given password.
func CreatePasswordVerification(password string) error {
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	path, err := GetVerifyFilePath()
	if err != nil {
		return fmt.Errorf("failed to get verify file path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, _, err := NewVerificationFileBytes(password)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write verification file: %w", err)
	}

	return nil
}

// DeriveKeyFromVault reads .dredge-key, derives the master key from password, and verifies it.
// Returns the master key if password is correct. Does NOT cache the key.
func DeriveKeyFromVault(password string) ([]byte, error) {
	if password == "" {
		return nil, fmt.Errorf("password cannot be empty")
	}

	path, err := GetVerifyFilePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get verify file path: %w", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("password verification file not found (run 'dredge add' to create vault)")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read verification file: %w", err)
	}

	if len(data) < SaltSize+NonceSize+16 {
		return nil, fmt.Errorf("verification file corrupted (too short)")
	}

	salt := data[:SaltSize]
	encrypted := data[SaltSize:]

	key := DeriveKey(password, salt)

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		return nil, fmt.Errorf("wrong password")
	}

	if string(decrypted) != VerificationContent {
		return nil, fmt.Errorf("verification file corrupted (unexpected content)")
	}

	return key, nil
}

// VerifyPassword checks if the given password is correct for the current vault.
func VerifyPassword(password string) error {
	_, err := DeriveKeyFromVault(password)
	return err
}

// GetKeyWithVerification is the main auth flow.
// Checks session cache; if miss, prompts password, verifies against .dredge-key, caches key.
// Returns the 32-byte master key.
func GetKeyWithVerification() ([]byte, error) {
	// Check session cache first
	cached, err := GetCachedKey()
	if err != nil {
		return nil, fmt.Errorf("failed to check key cache: %w", err)
	}

	if len(cached) == KeySize {
		return cached, nil
	}

	// No cached key — use pending password (from --password flag) or prompt
	var password string
	if pendingPassword != "" {
		password = pendingPassword
		pendingPassword = "" // clear immediately after use
	} else {
		var err error
		password, err = ui.PromptPassword()
		if err != nil {
			return nil, fmt.Errorf("failed to prompt for password: %w", err)
		}
	}

	if DebugMode {
		fmt.Fprintf(os.Stderr, "[DEBUG] password len=%d\n", len(password))
	}

	if password == "" {
		return nil, fmt.Errorf("password cannot be empty")
	}

	var key []byte

	if !PasswordVerificationExists() {
		// First time — create verification file
		path, err := GetVerifyFilePath()
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
		fileBytes, derivedKey, err := NewVerificationFileBytes(password)
		if err != nil {
			return nil, fmt.Errorf("failed to create password verification: %w", err)
		}
		if err := os.WriteFile(path, fileBytes, 0600); err != nil {
			return nil, fmt.Errorf("failed to write verification file: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Created password verification file")
		key = derivedKey
	} else {
		// Verify password and derive key
		derivedKey, err := DeriveKeyFromVault(password)
		if err != nil {
			return nil, err
		}
		key = derivedKey
	}

	// Cache the key
	if err := CacheKey(key); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cache key: %v\n", err)
	}

	return key, nil
}
