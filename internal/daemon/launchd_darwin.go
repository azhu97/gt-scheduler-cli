//go:build darwin

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"howett.net/plist"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
)

type launchdPlist struct {
	Label             string          `plist:"Label"`
	ProgramArguments  []string        `plist:"ProgramArguments"`
	RunAtLoad         bool            `plist:"RunAtLoad"`
	KeepAlive         map[string]bool `plist:"KeepAlive"`
	StandardOutPath   string          `plist:"StandardOutPath"`
	StandardErrorPath string          `plist:"StandardErrorPath"`
	ProcessType       string          `plist:"ProcessType"`
}

// LaunchdPlistPath is where the LaunchAgent plist lives.
func LaunchdPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel+".plist")
}

func launchdPlistDict(interval *int) (launchdPlist, error) {
	args, err := programArguments(interval)
	if err != nil {
		return launchdPlist{}, err
	}
	return launchdPlist{
		Label:             LaunchdLabel,
		ProgramArguments:  args,
		RunAtLoad:         true,
		KeepAlive:         map[string]bool{"SuccessfulExit": false},
		StandardOutPath:   config.LogPath(),
		StandardErrorPath: config.LogPath(),
		ProcessType:       "Background",
	}, nil
}

// IsLaunchdInstalled reports whether the LaunchAgent plist file exists.
func IsLaunchdInstalled() bool {
	_, err := os.Stat(LaunchdPlistPath())
	return err == nil
}

// InstallLaunchd writes a LaunchAgent plist and loads it: auto-starts at
// login, auto-restarts on a crash (nonzero exit), via launchd's KeepAlive.
func InstallLaunchd(interval *int) (string, error) {
	dict, err := launchdPlistDict(interval)
	if err != nil {
		return "", err
	}

	path := LaunchdPlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	enc := plist.NewEncoder(f)
	encErr := enc.Encode(dict)
	f.Close()
	if encErr != nil {
		return "", encErr
	}

	uid := os.Getuid()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Best-effort unload of any previous instance; ignore failure (it may
	// not be loaded yet).
	exec.CommandContext(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, LaunchdLabel)).Run()

	if out, err := exec.CommandContext(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), path).CombinedOutput(); err != nil {
		return "", fmt.Errorf("launchctl bootstrap failed: %s", string(out))
	}
	return path, nil
}

// UninstallLaunchd removes the LaunchAgent (stops it and disables auto-start).
func UninstallLaunchd() (bool, error) {
	if !IsLaunchdInstalled() {
		return false, nil
	}
	uid := os.Getuid()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exec.CommandContext(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, LaunchdLabel)).Run()
	if err := os.Remove(LaunchdPlistPath()); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}
