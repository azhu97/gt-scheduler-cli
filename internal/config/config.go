// Package config handles paths and the config file (~/.config/gtclass/config.toml).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultPollIntervalSeconds = 30
	DefaultNtfyServer          = "https://ntfy.sh"

	// SessionTermEnvVar is set by the interactive shell (internal/shell)
	// after its startup semester prompt, and read by cli.resolveTerm.
	// Session-scoped (this process only) — never written to config.toml,
	// so it doesn't leak into other invocations.
	SessionTermEnvVar = "GTCLASS_SESSION_TERM"
)

func ConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gtclass")
}

func DataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "gtclass")
}

func ConfigPath() string { return filepath.Join(ConfigDir(), "config.toml") }
func DBPath() string     { return filepath.Join(DataDir(), "gtclass.db") }
func PIDPath() string    { return filepath.Join(DataDir(), "daemon.pid") }
func LogPath() string    { return filepath.Join(DataDir(), "daemon.log") }
func StatePath() string  { return filepath.Join(DataDir(), "daemon_state.json") }

type NtfyConfig struct {
	Topic  string
	Server string
}

type Config struct {
	DefaultTerm         string // "" means unset
	PollIntervalSeconds int
	NotifyChannels      []string
	Ntfy                NtfyConfig
}

func defaultConfig() Config {
	return Config{
		PollIntervalSeconds: DefaultPollIntervalSeconds,
		NotifyChannels:      []string{"desktop"},
		Ntfy:                NtfyConfig{Server: DefaultNtfyServer},
	}
}

type rawNtfy struct {
	Topic  string `toml:"topic"`
	Server string `toml:"server"`
}

type rawNotify struct {
	Channels []string `toml:"channels"`
	Ntfy     rawNtfy  `toml:"ntfy"`
}

type rawConfig struct {
	DefaultTerm         string    `toml:"default_term"`
	PollIntervalSeconds int       `toml:"poll_interval_seconds"`
	Notify              rawNotify `toml:"notify"`
}

// LoadConfig reads config.toml, returning defaults if it doesn't exist.
func LoadConfig() (Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(ConfigPath())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}

	var raw rawConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", ConfigPath(), err)
	}

	cfg.DefaultTerm = raw.DefaultTerm
	if raw.PollIntervalSeconds != 0 {
		cfg.PollIntervalSeconds = raw.PollIntervalSeconds
	}
	if len(raw.Notify.Channels) > 0 {
		cfg.NotifyChannels = raw.Notify.Channels
	}
	if raw.Notify.Ntfy.Topic != "" {
		cfg.Ntfy.Topic = raw.Notify.Ntfy.Topic
	}
	if raw.Notify.Ntfy.Server != "" {
		cfg.Ntfy.Server = raw.Notify.Ntfy.Server
	}
	return cfg, nil
}

// SaveConfig writes config.toml with the same hand-rolled, minimal layout
// as the Python implementation (not a generic TOML marshaler) so the file
// stays comment-free and easy for a human to skim/hand-edit.
func SaveConfig(cfg Config) error {
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return err
	}

	var b strings.Builder
	if cfg.DefaultTerm != "" {
		fmt.Fprintf(&b, "default_term = %q\n", cfg.DefaultTerm)
	}
	fmt.Fprintf(&b, "poll_interval_seconds = %s\n", strconv.Itoa(cfg.PollIntervalSeconds))
	b.WriteString("\n")
	b.WriteString("[notify]\n")
	quoted := make([]string, len(cfg.NotifyChannels))
	for i, c := range cfg.NotifyChannels {
		quoted[i] = strconv.Quote(c)
	}
	fmt.Fprintf(&b, "channels = [%s]\n", strings.Join(quoted, ", "))
	b.WriteString("\n")
	b.WriteString("[notify.ntfy]\n")
	fmt.Fprintf(&b, "topic = %q\n", cfg.Ntfy.Topic)
	fmt.Fprintf(&b, "server = %q\n", cfg.Ntfy.Server)
	b.WriteString("\n")

	return os.WriteFile(ConfigPath(), []byte(b.String()), 0o644)
}
