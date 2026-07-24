package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureCreatesExactDefaultAndIsIdempotent(t *testing.T) {
	vault := t.TempDir()
	if err := Ensure(vault); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "dredge.toml")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(defaultConfig) {
		t.Fatalf("created config differs from embedded default:\n%s", got)
	}

	custom := append([]byte{}, got...)
	custom = append(custom, []byte("\n# user comment\n")...)
	if err := os.WriteFile(path, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(vault); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != string(custom) {
		t.Fatal("Ensure modified an existing valid configuration")
	}
}

func TestLoadDefault(t *testing.T) {
	vault := t.TempDir()
	if err := Ensure(vault); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(vault)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Format != 1 || cfg.History.Items.MaxVersions != 3 || cfg.History.Items.MaxBytesPerItem != 5000 {
		t.Fatalf("unexpected item config: %+v", cfg)
	}
	if cfg.History.Storage.MaxVersions != 1 || cfg.History.Storage.MaxBytesPerItem != 100_000_000 {
		t.Fatalf("unexpected storage config: %+v", cfg)
	}
	if cfg.History.Deleted.RetainFor != 30*24*time.Hour {
		t.Fatalf("unexpected deletion retention: %s", cfg.History.Deleted.RetainFor)
	}
}

func TestEnsureRejectsInvalidExistingConfigWithoutReplacingIt(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "dredge.toml")
	invalid := []byte("format = 1\n")
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	err := Ensure(vault)
	if err == nil || !strings.Contains(err.Error(), "history.items.max_versions") {
		t.Fatalf("expected field-specific error, got %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(invalid) {
		t.Fatal("invalid existing config was replaced")
	}
}

func TestLoadValidationErrors(t *testing.T) {
	base := string(defaultConfig)
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{"format", strings.Replace(base, "format = 1", "format = 2", 1), "format must be 1"},
		{"item versions", strings.Replace(base, "max_versions = 3", "max_versions = 0", 1), "history.items.max_versions"},
		{"item bytes", strings.Replace(base, `"5kb"`, `"0"`, 1), "history.items.max_bytes_per_item"},
		{"storage bytes", strings.Replace(base, `"100mb"`, `"1tb"`, 1), "history.storage.max_bytes_per_item"},
		{"duration", strings.Replace(base, `"30d"`, `"-1d"`, 1), "history.deleted.retain_for"},
		{"missing field", strings.Replace(base, `retain_for = "30d"`, "", 1), "history.deleted.retain_for"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vault := t.TempDir()
			path := filepath.Join(vault, "dredge.toml")
			if err := os.WriteFile(path, []byte(tt.config), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(vault)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	valid := map[string]int64{
		"1": 1, "5KB": 5000, "2 mb": 2_000_000, "3GB": 3_000_000_000,
		"5KiB": 5120, "2MIB": 2 * 1024 * 1024, "1gib": 1024 * 1024 * 1024,
	}
	for input, want := range valid {
		got, err := ParseByteSize(input)
		if err != nil || got != want {
			t.Errorf("ParseByteSize(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "-1", "1.5kb", "1tb", "kb", "1kb junk", "999999999999999999999999GB"} {
		if _, err := ParseByteSize(input); err == nil {
			t.Errorf("ParseByteSize(%q) unexpectedly succeeded", input)
		}
	}
}

func TestParseDuration(t *testing.T) {
	valid := map[string]time.Duration{
		"0d": 0, "1h": time.Hour, "2D": 48 * time.Hour,
		"1w": 7 * 24 * time.Hour, "1w 2d 3h": (7*24 + 2*24 + 3) * time.Hour,
	}
	for input, want := range valid {
		got, err := ParseDuration(input)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %s, %v; want %s", input, got, err, want)
		}
	}
	for _, input := range []string{"", "-1d", "1.5h", "1m", "1d junk", "d", "999999999999999999999w"} {
		if _, err := ParseDuration(input); err == nil {
			t.Errorf("ParseDuration(%q) unexpectedly succeeded", input)
		}
	}
}
