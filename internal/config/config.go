package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	supportedFormatVersion = 1
	configDirName          = ".dredge"
	configFileName         = "config.toml"
	configDirPermissions   = 0700
	configFilePermissions  = 0600
	decimalBase            = 1000
	binaryBase             = 1024
	hoursPerDay            = 24
	daysPerWeek            = 7
	maxInt64               = int64(^uint64(0) >> 1)
	configTempFilePattern  = ".config-*.tmp"
)

//go:embed default.toml
var defaultConfig []byte

var requiredFields = [][]string{
	{"format"},
	{"history", "items", "max_versions"},
	{"history", "items", "max_bytes_per_item"},
	{"history", "storage", "max_versions"},
	{"history", "storage", "max_bytes_per_item"},
	{"history", "deleted", "retain_for"},
}

type Config struct {
	Format  int
	History HistoryConfig
}

type HistoryConfig struct {
	Items   RetentionConfig
	Storage RetentionConfig
	Deleted DeletedConfig
}

type RetentionConfig struct {
	MaxVersions     int
	MaxBytesPerItem int64
}

type DeletedConfig struct {
	RetainFor time.Duration
}

type fileConfig struct {
	Format  int               `toml:"format"`
	History fileHistoryConfig `toml:"history"`
}

type fileHistoryConfig struct {
	Items   fileRetentionConfig `toml:"items"`
	Storage fileRetentionConfig `toml:"storage"`
	Deleted fileDeletedConfig   `toml:"deleted"`
}

type fileRetentionConfig struct {
	MaxVersions     int    `toml:"max_versions"`
	MaxBytesPerItem string `toml:"max_bytes_per_item"`
}

type fileDeletedConfig struct {
	RetainFor string `toml:"retain_for"`
}

var (
	byteSizePattern = regexp.MustCompile(`^([0-9]+)\s*([a-zA-Z]*)$`)
	durationPattern = regexp.MustCompile(`([0-9]+)\s*([hHdDwW])`)
	byteMultipliers = map[string]int64{
		"":    1,
		"kb":  decimalBase,
		"mb":  decimalBase * decimalBase,
		"gb":  decimalBase * decimalBase * decimalBase,
		"kib": binaryBase,
		"mib": binaryBase * binaryBase,
		"gib": binaryBase * binaryBase * binaryBase,
	}
	durationMultipliers = map[string]time.Duration{
		"h": time.Hour,
		"d": hoursPerDay * time.Hour,
		"w": daysPerWeek * hoursPerDay * time.Hour,
	}
)

// Ensure creates the default configuration when absent, then loads and validates it.
func Ensure(vaultDir string) error {
	path := filepath.Join(vaultDir, configDirName, configFileName)
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to inspect vault configuration: %w", err)
		}
		if err := createDefault(path); err != nil {
			return err
		}
	}
	_, err := Load(vaultDir)
	return err
}

// Load reads and validates a vault configuration.
func Load(vaultDir string) (Config, error) {
	path := filepath.Join(vaultDir, configDirName, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read vault configuration: %w", err)
	}

	var raw fileConfig
	meta, err := toml.Decode(string(data), &raw)
	if err != nil {
		return Config{}, fmt.Errorf("invalid vault configuration: %w", err)
	}

	for _, field := range requiredFields {
		if !meta.IsDefined(field...) {
			return Config{}, fmt.Errorf("invalid vault configuration: missing %s", strings.Join(field, "."))
		}
	}

	if raw.Format != supportedFormatVersion {
		return Config{}, fmt.Errorf("invalid vault configuration: format must be %d", supportedFormatVersion)
	}
	items, err := validateRetention("history.items", raw.History.Items)
	if err != nil {
		return Config{}, err
	}
	storage, err := validateRetention("history.storage", raw.History.Storage)
	if err != nil {
		return Config{}, err
	}
	retainFor, err := ParseDuration(raw.History.Deleted.RetainFor)
	if err != nil {
		return Config{}, fmt.Errorf("invalid vault configuration: history.deleted.retain_for: %w", err)
	}

	return Config{
		Format: raw.Format,
		History: HistoryConfig{
			Items:   items,
			Storage: storage,
			Deleted: DeletedConfig{RetainFor: retainFor},
		},
	}, nil
}

func validateRetention(name string, raw fileRetentionConfig) (RetentionConfig, error) {
	if raw.MaxVersions <= 0 {
		return RetentionConfig{}, fmt.Errorf("invalid vault configuration: %s.max_versions must be greater than 0", name)
	}
	maxBytes, err := ParseByteSize(raw.MaxBytesPerItem)
	if err != nil {
		return RetentionConfig{}, fmt.Errorf("invalid vault configuration: %s.max_bytes_per_item: %w", name, err)
	}
	if maxBytes <= 0 {
		return RetentionConfig{}, fmt.Errorf("invalid vault configuration: %s.max_bytes_per_item must be greater than 0", name)
	}
	return RetentionConfig{MaxVersions: raw.MaxVersions, MaxBytesPerItem: maxBytes}, nil
}

// ParseByteSize parses byte counts with decimal KB/MB/GB or binary KiB/MiB/GiB units.
func ParseByteSize(value string) (int64, error) {
	match := byteSizePattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, fmt.Errorf("must be a whole byte count with an optional KB, MB, GB, KiB, MiB, or GiB unit")
	}
	number, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("byte count is out of range")
	}
	multiplier, ok := byteMultipliers[strings.ToLower(match[2])]
	if !ok {
		return 0, fmt.Errorf("unsupported byte unit %q", match[2])
	}
	if number > maxInt64/multiplier {
		return 0, fmt.Errorf("byte count is out of range")
	}
	return number * multiplier, nil
}

// ParseDuration parses a sequence of whole hours, days, and weeks.
func ParseDuration(value string) (time.Duration, error) {
	input := strings.TrimSpace(value)
	if input == "" {
		return 0, fmt.Errorf("must contain hours, days, or weeks")
	}
	var total time.Duration
	pos := 0
	for _, match := range durationPattern.FindAllStringSubmatchIndex(input, -1) {
		if strings.TrimSpace(input[pos:match[0]]) != "" {
			return 0, fmt.Errorf("must contain only whole hours, days, or weeks")
		}
		number, err := strconv.ParseUint(input[match[2]:match[3]], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("duration is out of range")
		}
		unit := strings.ToLower(input[match[4]:match[5]])
		factor := durationMultipliers[unit]
		if number > uint64(maxInt64/int64(factor)) || total > time.Duration(maxInt64)-time.Duration(number)*factor {
			return 0, fmt.Errorf("duration is out of range")
		}
		total += time.Duration(number) * factor
		pos = match[1]
	}
	if pos == 0 || strings.TrimSpace(input[pos:]) != "" {
		return 0, fmt.Errorf("must contain only whole hours, days, or weeks")
	}
	return total, nil
}

func createDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), configDirPermissions); err != nil {
		return fmt.Errorf("failed to create vault configuration directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), configTempFilePattern)
	if err != nil {
		return fmt.Errorf("failed to create vault configuration: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(configFilePermissions); err != nil {
		temp.Close()
		return fmt.Errorf("failed to set vault configuration permissions: %w", err)
	}
	if _, err := temp.Write(defaultConfig); err != nil {
		temp.Close()
		return fmt.Errorf("failed to write vault configuration: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("failed to sync vault configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close vault configuration: %w", err)
	}
	// A hard link installs the completed file atomically and cannot replace a
	// configuration another process created after our initial absence check.
	if err := os.Link(tempPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			_, loadErr := Load(filepath.Dir(filepath.Dir(path)))
			return loadErr
		}
		return fmt.Errorf("failed to install vault configuration: %w", err)
	}
	return nil
}
