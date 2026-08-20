//go:build darwin

package daemon

import "testing"

// These, like the Python original's test_daemon.py, only test plist
// *construction* — no test here exercises real launchd (launchctl
// bootstrap/bootout), since that's only meaningfully verifiable by hand on
// a real macOS login session.

func TestLaunchdPlistDictDefaults(t *testing.T) {
	dict, err := launchdPlistDict(nil)
	if err != nil {
		t.Fatal(err)
	}
	if dict.Label != LaunchdLabel {
		t.Errorf("Label = %q, want %q", dict.Label, LaunchdLabel)
	}
	if len(dict.ProgramArguments) < 3 {
		t.Fatalf("ProgramArguments too short: %v", dict.ProgramArguments)
	}
	tail := dict.ProgramArguments[len(dict.ProgramArguments)-3:]
	want := []string{"daemon", "start", "--foreground"}
	for i := range want {
		if tail[i] != want[i] {
			t.Errorf("ProgramArguments tail = %v, want %v", tail, want)
		}
	}
	if !dict.RunAtLoad {
		t.Error("RunAtLoad = false, want true")
	}
	if dict.KeepAlive["SuccessfulExit"] != false {
		t.Errorf("KeepAlive[SuccessfulExit] = %v, want false", dict.KeepAlive["SuccessfulExit"])
	}
	if dict.StandardOutPath != dict.StandardErrorPath {
		t.Errorf("StandardOutPath (%q) != StandardErrorPath (%q)", dict.StandardOutPath, dict.StandardErrorPath)
	}
}

func TestLaunchdPlistDictWithInterval(t *testing.T) {
	interval := 45
	dict, err := launchdPlistDict(&interval)
	if err != nil {
		t.Fatal(err)
	}
	tail := dict.ProgramArguments[len(dict.ProgramArguments)-5:]
	want := []string{"daemon", "start", "--foreground", "--interval", "45"}
	for i := range want {
		if tail[i] != want[i] {
			t.Errorf("ProgramArguments tail = %v, want %v", tail, want)
		}
	}
}
