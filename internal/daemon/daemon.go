// Package daemon manages the background poller lifecycle: `gtclass daemon
// start/stop/status`.
//
// Like the Python original, this is a plain sleep loop (no scheduler
// dependency), optionally detached into the background via a re-exec'd
// child process and a PID file under the data dir — that covers "run
// poller in background" without extra process-management machinery.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/dbstore"
	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
	"github.com/azhu97/gt-scheduler-cli/internal/poller"
)

const LaunchdLabel = "com.gtclass.daemon"

type StatusInfo struct {
	Running        bool
	PID            int
	StartedAt      string
	PollInterval   int
	LastPolledAt   string
	LastPollCRNs   int
	LastPollErrors int
	LastError      string
	LastErrorAt    string
}

// IsRunning probes liveness via signal 0, same as `os.kill(pid, 0)` in Python.
func IsRunning(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// isGtclassProcess is a best-effort guard against a reused PID: checks the
// live process's command line actually looks like a gtclass daemon, not
// just that *some* process with this PID exists. Fails open (returns true)
// if it can't verify, so a verification hiccup never false-negatives a
// healthy daemon.
func isGtclassProcess(pid int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return true
	}
	return strings.Contains(string(out), "gtclass")
}

// ReadPID returns the PID recorded in the PID file, or 0 if there isn't one.
func ReadPID() int {
	data, err := os.ReadFile(config.PIDPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// Status reports whether the daemon is running, cleaning up stale PID/state
// files if the recorded PID isn't actually a live gtclass process.
func Status() (running bool, pid int) {
	pid = ReadPID()
	if pid == 0 {
		return false, 0
	}
	if IsRunning(pid) && isGtclassProcess(pid) {
		return true, pid
	}
	os.Remove(config.PIDPath())
	os.Remove(config.StatePath())
	return false, 0
}

func readState() map[string]any {
	data, err := os.ReadFile(config.StatePath())
	if err != nil {
		return map[string]any{}
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil || state == nil {
		return map[string]any{}
	}
	return state
}

func writeState(updates map[string]any) {
	state := readState()
	for k, v := range updates {
		state[k] = v
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	os.WriteFile(config.StatePath(), data, 0o644)
}

func stateString(state map[string]any, key string) string {
	if v, ok := state[key].(string); ok {
		return v
	}
	return ""
}

func stateInt(state map[string]any, key string) int {
	if v, ok := state[key].(float64); ok { // json numbers decode as float64
		return int(v)
	}
	return 0
}

// StatusDetail returns full daemon status plus the last-poll bookkeeping
// recorded in daemon_state.json.
func StatusDetail() StatusInfo {
	running, pid := Status()
	var state map[string]any
	if running {
		state = readState()
	} else {
		state = map[string]any{}
	}
	return StatusInfo{
		Running:        running,
		PID:            pid,
		StartedAt:      stateString(state, "started_at"),
		PollInterval:   stateInt(state, "poll_interval"),
		LastPolledAt:   stateString(state, "last_polled_at"),
		LastPollCRNs:   stateInt(state, "last_poll_crns"),
		LastPollErrors: stateInt(state, "last_poll_errors"),
		LastError:      stateString(state, "last_error"),
		LastErrorAt:    stateString(state, "last_error_at"),
	}
}

// Stop sends SIGTERM and waits (up to 2s) for the process to exit.
func Stop() bool {
	running, pid := Status()
	if !running {
		return false
	}
	syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		if !IsRunning(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	os.Remove(config.PIDPath())
	os.Remove(config.StatePath())
	return true
}

// RunForeground is the blocking poll loop. It installs a SIGTERM handler
// (via signal.NotifyContext) so Stop() triggers a graceful exit within
// ~100ms rather than waiting out a full poll interval.
func RunForeground(parentCtx context.Context, interval *int, onTick func([]poller.PollEvent, []poller.PollError)) error {
	ctx, stop := signal.NotifyContext(parentCtx, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(filepath.Dir(config.PIDPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(config.PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	defer os.Remove(config.PIDPath())
	defer os.Remove(config.StatePath())

	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	pollInterval := cfg.PollIntervalSeconds
	if interval != nil && *interval > 0 {
		pollInterval = *interval
	}

	writeState(map[string]any{
		"started_at":       time.Now().UTC().Format(time.RFC3339),
		"poll_interval":    pollInterval,
		"last_polled_at":   nil,
		"last_poll_crns":   nil,
		"last_poll_errors": nil,
		"last_error":       nil,
		"last_error_at":    nil,
	})

	db, err := dbstore.Connect("")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := dbstore.InitDB(db); err != nil {
		return err
	}
	client := gtdata.NewClient()

	for ctx.Err() == nil {
		events, errs, pollErr := poller.PollOnce(ctx, db, client, cfg)
		if pollErr != nil {
			fmt.Fprintln(os.Stderr, pollErr)
			writeState(map[string]any{
				"last_error":    pollErr.Error(),
				"last_error_at": time.Now().UTC().Format(time.RFC3339),
			})
		} else {
			writeState(map[string]any{
				"last_polled_at":   time.Now().UTC().Format(time.RFC3339),
				"last_poll_crns":   len(events),
				"last_poll_errors": len(errs),
			})
			if onTick != nil {
				onTick(events, errs)
			}
		}

		deadline := time.Now().Add(time.Duration(pollInterval) * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	return nil
}

// StartDetached re-execs the current binary as `daemon start --foreground`,
// detached from the controlling terminal (Setsid) with output redirected
// to the daemon log, and returns its PID.
func StartDetached(interval *int) (int, error) {
	running, pid := Status()
	if running {
		return pid, nil
	}

	if err := os.MkdirAll(filepath.Dir(config.LogPath()), 0o755); err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	args := []string{"daemon", "start", "--foreground"}
	if interval != nil && *interval > 0 {
		args = append(args, "--interval", strconv.Itoa(*interval))
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// programArguments builds the ProgramArguments used both to re-exec the
// daemon detached and in the launchd plist.
func programArguments(interval *int) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{exe, "daemon", "start", "--foreground"}
	if interval != nil && *interval > 0 {
		args = append(args, "--interval", strconv.Itoa(*interval))
	}
	return args, nil
}
