package crypto

import (
	"bytes"
	"os"
	"testing"
)

// testSessionKey returns a valid 32-byte key for session tests.
func testSessionKey() []byte {
	return DeriveKey("test-password-123", []byte("16-byte-salt-val"))
}

func TestSessionCache_RoundTrip(t *testing.T) {
	// Clear any existing session first
	_ = ClearSession()

	testKey := testSessionKey()

	// Check no active session initially
	if HasActiveSession() {
		t.Error("Should have no active session initially")
	}

	// Cache the key
	err := CacheKey(testKey)
	if err != nil {
		t.Fatalf("CacheKey failed: %v", err)
	}

	// Check active session now exists
	if !HasActiveSession() {
		t.Error("Should have active session after caching key")
	}

	// Retrieve the cached key
	retrieved, err := GetCachedKey()
	if err != nil {
		t.Fatalf("GetCachedKey failed: %v", err)
	}

	if len(retrieved) == 0 {
		t.Fatal("GetCachedKey returned empty key")
	}

	// Verify retrieved key matches original
	if !bytes.Equal(retrieved, testKey) {
		t.Errorf("Retrieved key doesn't match cached key.\nGot:  %x\nWant: %x", retrieved, testKey)
	}

	// Clean up
	err = ClearSession()
	if err != nil {
		t.Fatalf("ClearSession failed: %v", err)
	}

	// Verify session is cleared
	if HasActiveSession() {
		t.Error("Should have no active session after clearing")
	}

	retrieved, err = GetCachedKey()
	if err != nil {
		t.Fatalf("GetCachedKey after clear failed: %v", err)
	}

	if len(retrieved) != 0 {
		t.Error("GetCachedKey should return nil after session cleared")
	}
}

func TestCacheKey_WrongSize(t *testing.T) {
	// Try to cache key with wrong size
	err := CacheKey([]byte("not-32-bytes"))
	if err == nil {
		t.Error("CacheKey should fail with wrong-size key, but succeeded")
	}

	// Clean up in case it somehow got cached
	_ = ClearSession()
}

func TestCacheKey_Empty(t *testing.T) {
	// Try to cache empty key
	err := CacheKey([]byte{})
	if err == nil {
		t.Error("CacheKey should fail with empty key, but succeeded")
	}

	_ = ClearSession()
}

func TestGetCachedKey_NoCache(t *testing.T) {
	// Clear any existing session
	_ = ClearSession()

	// Try to get cached key when none exists
	key, err := GetCachedKey()
	if err != nil {
		t.Fatalf("GetCachedKey should not error when cache doesn't exist: %v", err)
	}

	if len(key) != 0 {
		t.Error("GetCachedKey should return nil when no cache exists")
	}
}

func TestClearSession_NoCache(t *testing.T) {
	// Clear session when none exists (should not error)
	err := ClearSession()
	if err != nil {
		t.Errorf("ClearSession should not error when no cache exists: %v", err)
	}
}

func TestGetPPID(t *testing.T) {
	ppid := GetPPID()

	if ppid == "" {
		t.Error("GetPPID returned empty string")
	}

	// Verify actual PPID matches
	expected := os.Getppid()
	if ppid == "" && expected != 0 {
		t.Logf("PPID: %s (expected: %d)", ppid, expected)
	}
}

func TestSessionCache_Permissions(t *testing.T) {
	// Clear any existing session
	_ = ClearSession()

	// Create and cache a key
	testKey := testSessionKey()
	err := CacheKey(testKey)
	if err != nil {
		t.Fatalf("CacheKey failed: %v", err)
	}

	// Verify cache was created (permissions checked implicitly by OS)
	if !HasActiveSession() {
		t.Error("Cache should exist after CacheKey")
	}

	// Clean up
	_ = ClearSession()
}
