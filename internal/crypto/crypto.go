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

	"github.com/DeprecatedLuar/dredge/internal/ui"
)

// Debug mode flag (set from main)
var DebugMode bool

// ============================================================================
// Constants
// ============================================================================

// Encryption constants
const (
	SaltSize  = 16 // 128 bits
	NonceSize = 12 // 96 bits (standard GCM nonce size)

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

// Encrypt encrypts plaintext using password-derived key (Argon2id + AES-256-GCM).
// Returns binary format: [16B salt][12B nonce][N bytes ciphertext + 16B auth tag]
func Encrypt(plaintext []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, fmt.Errorf("password cannot be empty")
	}

	// Generate random salt
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key from password using Argon2id
	key := argon2.IDKey(
		[]byte(password),
		salt,
		Argon2Time,
		Argon2Memory,
		Argon2Threads,
		Argon2KeyLength,
	)

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Construct final format: salt || nonce || ciphertext
	result := make([]byte, 0, SaltSize+NonceSize+len(ciphertext))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// Decrypt decrypts encrypted data using password.
// Checks session cache first, prompts for password if needed.
// Input format: [16B salt][12B nonce][N bytes ciphertext + 16B auth tag]
func Decrypt(encrypted []byte, password string) ([]byte, error) {
	// Validate minimum size: salt + nonce + auth tag
	minSize := SaltSize + NonceSize + 16 // 16 = GCM auth tag size
	if len(encrypted) < minSize {
		return nil, fmt.Errorf("encrypted data too short: got %d bytes, need at least %d", len(encrypted), minSize)
	}

	// Extract salt, nonce, and ciphertext
	salt := encrypted[:SaltSize]
	nonce := encrypted[SaltSize : SaltSize+NonceSize]
	ciphertext := encrypted[SaltSize+NonceSize:]

	// Try to get cached password first
	cachedPassword, err := GetCachedPassword()
	if err != nil {
		return nil, fmt.Errorf("failed to check session cache: %w", err)
	}

	// Use cached password if available, otherwise use provided password
	if cachedPassword != "" {
		if DebugMode {
			fmt.Fprintf(os.Stderr, "[DEBUG] Decrypt: using CACHED password %q instead of provided %q\n", cachedPassword, password)
		}
		password = cachedPassword
	} else if password == "" {
		return nil, fmt.Errorf("no cached password and no password provided")
	}
	// NOTE: Don't cache password here - let caller cache after successful verification

	// Derive key from password + file's unique salt
	key := argon2.IDKey(
		[]byte(password),
		salt,
		Argon2Time,
		Argon2Memory,
		Argon2Threads,
		Argon2KeyLength,
	)

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt and verify authentication tag
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong password or tampered data): %w", err)
	}

	return plaintext, nil
}

// DeriveKey derives an encryption key from password and salt using Argon2id.
// Used for manual key derivation when needed.
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

// ============================================================================
// Session Management
// ============================================================================

// Session cache configuration
const (
	tempDirBase      = "/tmp/dredge"
	sessionCacheFile = ".session" // Hidden file for security
)

// getSessionDir returns the session-specific directory path
func getSessionDir() string {
	return filepath.Join(tempDirBase, fmt.Sprintf("%d", os.Getppid()))
}

// GetCachedPassword retrieves the cached password from session cache.
// Returns empty string if cache doesn't exist.
func GetCachedPassword() (string, error) {
	cachePath := filepath.Join(getSessionDir(), sessionCacheFile)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // Cache doesn't exist, not an error
		}
		return "", fmt.Errorf("failed to read session cache: %w", err)
	}

	return string(data), nil
}

// CachePassword stores the password in session cache.
func CachePassword(password string) error {
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	// Ensure session directory exists
	sessionDir := getSessionDir()
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	cachePath := filepath.Join(sessionDir, sessionCacheFile)
	if err := os.WriteFile(cachePath, []byte(password), 0600); err != nil {
		return fmt.Errorf("failed to cache password: %w", err)
	}

	return nil
}

// ClearSession removes the session cache file.
func ClearSession() error {
	cachePath := filepath.Join(getSessionDir(), sessionCacheFile)
	err := os.Remove(cachePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear session cache: %w", err)
	}
	return nil
}

// HasActiveSession checks if a session cache exists for current terminal.
func HasActiveSession() bool {
	password, err := GetCachedPassword()
	return err == nil && password != ""
}

// GetPPID returns the parent process ID (for debugging/testing).
func GetPPID() string {
	return strconv.Itoa(os.Getppid())
}

// ============================================================================
// Password Verification
// ============================================================================

// GetVerifyFilePath returns the full path to the password verification file
func GetVerifyFilePath() (string, error) {
	// Use XDG_DATA_HOME or default to ~/.local/share
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

// PasswordVerificationExists checks if the .dredge-key file exists
func PasswordVerificationExists() bool {
	path, err := GetVerifyFilePath()
	if err != nil {
		return false
	}

	_, err = os.Stat(path)
	return err == nil
}

// CreatePasswordVerification creates the .dredge-key file with the given password
func CreatePasswordVerification(password string) error {
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	path, err := GetVerifyFilePath()
	if err != nil {
		return fmt.Errorf("failed to get verify file path: %w", err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Encrypt the verification content
	encrypted, err := Encrypt([]byte(VerificationContent), password)
	if err != nil {
		return fmt.Errorf("failed to encrypt verification data: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, encrypted, 0600); err != nil {
		return fmt.Errorf("failed to write verification file: %w", err)
	}

	return nil
}

// VerifyPassword attempts to decrypt .dredge-key with the given password
// Returns nil if password is correct, error otherwise
func VerifyPassword(password string) error {
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	path, err := GetVerifyFilePath()
	if err != nil {
		return fmt.Errorf("failed to get verify file path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("password verification file not found (run 'dredge add' to create vault)")
	}

	// Read encrypted data
	encrypted, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read verification file: %w", err)
	}

	// Try to decrypt WITHOUT using session cache
	// We need to verify THIS specific password, not fall back to cache
	// So we temporarily clear cache, decrypt, then restore if needed
	cachedPassword, _ := GetCachedPassword()
	if DebugMode {
		fmt.Fprintf(os.Stderr, "[DEBUG] VerifyPassword: cached=%q, provided=%q\n", cachedPassword, password)
	}
	_ = ClearSession() // Clear cache temporarily

	decrypted, err := Decrypt(encrypted, password)

	// Restore cache if there was one (and it's different from test password)
	if cachedPassword != "" && cachedPassword != password {
		_ = CachePassword(cachedPassword)
	}

	if err != nil {
		// Decryption failed = wrong password
		return fmt.Errorf("wrong password")
	}

	// Verify content matches expected value
	if string(decrypted) != VerificationContent {
		return fmt.Errorf("verification file corrupted (expected %q, got %q)", VerificationContent, string(decrypted))
	}

	return nil
}

// GetPasswordWithVerification prompts for password and verifies it against .dredge-key
// If .dredge-key doesn't exist, creates it with the entered password
func GetPasswordWithVerification() (string, error) {
	// Check session cache first
	cached, err := GetCachedPassword()
	if err != nil {
		return "", fmt.Errorf("failed to check password cache: %w", err)
	}

	// If cache exists, trust it - it was already verified when cached
	if cached != "" {
		return cached, nil
	}

	// No cached password, prompt user
	password, err := ui.PromptPassword()
	if err != nil {
		return "", fmt.Errorf("failed to prompt for password: %w", err)
	}

	// DEBUG: show what we received
	if DebugMode {
		fmt.Fprintf(os.Stderr, "[DEBUG] password len=%d bytes=%v\n", len(password), []byte(password))
	}

	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	// Check if verification file exists
	if !PasswordVerificationExists() {
		// First time - create verification file
		if err := CreatePasswordVerification(password); err != nil {
			return "", fmt.Errorf("failed to create password verification: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Created password verification file")
	} else {
		// Verify password
		if err := VerifyPassword(password); err != nil {
			return "", err
		}
	}

	// Cache the verified password
	if err := CachePassword(password); err != nil {
		// Non-fatal: just warn
		fmt.Fprintf(os.Stderr, "Warning: failed to cache password: %v\n", err)
	}

	return password, nil
}
