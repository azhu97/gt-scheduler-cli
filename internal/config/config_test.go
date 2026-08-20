package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func withScratchDirs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
}

func TestSaveConfigRoundtrip(t *testing.T) {
	withScratchDirs(t)

	cfg := Config{
		DefaultTerm:         "202608",
		PollIntervalSeconds: 45,
		NotifyChannels:      []string{"desktop", "ntfy"},
		Ntfy:                NtfyConfig{Topic: "my-topic", Server: "https://ntfy.example.com"},
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(ConfigPath()); err != nil {
		t.Fatalf("config file wasn't written: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, cfg) {
		t.Errorf("loaded = %+v, want %+v", loaded, cfg)
	}
}

func TestLoadConfigDefaultsWhenMissing(t *testing.T) {
	withScratchDirs(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollIntervalSeconds != DefaultPollIntervalSeconds {
		t.Errorf("PollIntervalSeconds = %d, want default %d", cfg.PollIntervalSeconds, DefaultPollIntervalSeconds)
	}
	if len(cfg.NotifyChannels) != 1 || cfg.NotifyChannels[0] != "desktop" {
		t.Errorf("NotifyChannels = %v, want [desktop]", cfg.NotifyChannels)
	}
	if cfg.Ntfy.Server != DefaultNtfyServer {
		t.Errorf("Ntfy.Server = %q, want default %q", cfg.Ntfy.Server, DefaultNtfyServer)
	}
}
