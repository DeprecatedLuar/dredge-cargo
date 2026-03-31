package crypto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeprecatedLuar/dredge/internal/session"
)

func TestCreateAndVerifyPassword(t *testing.T) {
	// Setup test environment
	_ = ClearSession()

	// Set temp XDG directory
	tmpDir, err := os.MkdirTemp("", "dredge-verify-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)
	session.SetVaultPath(filepath.Join(tmpDir, "dredge"))

	testPassword := "test-password-123"

	// Should not exist initially
	if PasswordVerificationExists() {
		t.Error("Verification file should not exist initially")
	}

	// Create verification
	if err := CreatePasswordVerification(testPassword); err != nil {
		t.Fatalf("CreatePasswordVerification failed: %v", err)
	}

	// Should exist now
	if !PasswordVerificationExists() {
		t.Error("Verification file should exist after creation")
	}

	// Verify with correct password
	if err := VerifyPassword(testPassword); err != nil {
		t.Errorf("VerifyPassword with correct password failed: %v", err)
	}

	// Verify with wrong password
	if err := VerifyPassword("wrong-password"); err == nil {
		t.Error("VerifyPassword should fail with wrong password")
	}
}

func TestVerifyPassword_FileNotFound(t *testing.T) {
	// Setup test environment
	tmpDir, err := os.MkdirTemp("", "dredge-verify-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", oldXDG)
	session.SetVaultPath(filepath.Join(tmpDir, "dredge"))

	// Try to verify when file doesn't exist
	err = VerifyPassword("any-password")
	if err == nil {
		t.Error("VerifyPassword should fail when file doesn't exist")
	}
}

func TestCreatePasswordVerification_EmptyPassword(t *testing.T) {
	err := CreatePasswordVerification("")
	if err == nil {
		t.Error("CreatePasswordVerification should fail with empty password")
	}
}

func TestVerifyPassword_EmptyPassword(t *testing.T) {
	err := VerifyPassword("")
	if err == nil {
		t.Error("VerifyPassword should fail with empty password")
	}
}

func TestPasswordVerificationUniqueSalts(t *testing.T) {
	// Setup test environment
	_ = ClearSession()

	tmpDir1, err := os.MkdirTemp("", "dredge-verify-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir1)

	tmpDir2, err := os.MkdirTemp("", "dredge-verify-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir2)

	oldXDG := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", oldXDG)

	password := "same-password"

	// Create first verification file
	os.Setenv("XDG_DATA_HOME", tmpDir1)
	session.SetVaultPath(filepath.Join(tmpDir1, "dredge"))
	if err := CreatePasswordVerification(password); err != nil {
		t.Fatalf("First CreatePasswordVerification failed: %v", err)
	}
	path1, _ := GetVerifyFilePath()
	data1, _ := os.ReadFile(path1)

	// Create second verification file
	os.Setenv("XDG_DATA_HOME", tmpDir2)
	session.SetVaultPath(filepath.Join(tmpDir2, "dredge"))
	if err := CreatePasswordVerification(password); err != nil {
		t.Fatalf("Second CreatePasswordVerification failed: %v", err)
	}
	path2, _ := GetVerifyFilePath()
	data2, _ := os.ReadFile(path2)

	// Files should have different content (different salts/nonces)
	if string(data1) == string(data2) {
		t.Error("Verification files should have different content due to unique salts")
	}

	// But both should verify with the same password
	os.Setenv("XDG_DATA_HOME", tmpDir1)
	session.SetVaultPath(filepath.Join(tmpDir1, "dredge"))
	if err := VerifyPassword(password); err != nil {
		t.Errorf("First file verification failed: %v", err)
	}

	os.Setenv("XDG_DATA_HOME", tmpDir2)
	session.SetVaultPath(filepath.Join(tmpDir2, "dredge"))
	if err := VerifyPassword(password); err != nil {
		t.Errorf("Second file verification failed: %v", err)
	}
}
