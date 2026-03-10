package app

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

var deviceIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestLoadConfigGeneratesDeviceIDOnFirstLoad(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DeviceID == "" {
		t.Fatal("LoadConfig() returned an empty device ID")
	}
	if !deviceIDPattern.MatchString(cfg.DeviceID) {
		t.Fatalf("LoadConfig() returned device ID %q, want UUID-like value", cfg.DeviceID)
	}

	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", path, err)
	}

	var persisted Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(config) error = %v", err)
	}
	if persisted.DeviceID != cfg.DeviceID {
		t.Fatalf("persisted device ID = %q, want %q", persisted.DeviceID, cfg.DeviceID)
	}
}

func TestLoadConfigBackfillsMissingDeviceID(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	cfg := DefaultConfig()
	if cfg.DeviceID != "" {
		t.Fatalf("DefaultConfig().DeviceID = %q, want empty before persistence", cfg.DeviceID)
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if reloaded.DeviceID == "" {
		t.Fatal("LoadConfig() did not backfill a missing device ID")
	}
	if !deviceIDPattern.MatchString(reloaded.DeviceID) {
		t.Fatalf("reloaded device ID = %q, want UUID-like value", reloaded.DeviceID)
	}

	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", path, err)
	}

	var persisted Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(config) error = %v", err)
	}
	if persisted.DeviceID != reloaded.DeviceID {
		t.Fatalf("persisted device ID = %q, want %q", persisted.DeviceID, reloaded.DeviceID)
	}
}
