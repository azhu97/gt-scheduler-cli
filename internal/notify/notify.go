// Package notify dispatches to pluggable notification channels.
//
// Multiple channels can be active at once (config, not a single choice) so
// a missed device doesn't mean a missed spot. MVP ships two zero/low-effort
// channels; more (email, Discord, Pushover, Telegram, SMS) can be added
// later behind the same channel-function-plus-registry-entry shape.
package notify

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
)

// Result is (channel, error) for one notify attempt; Err is nil on success.
type Result struct {
	Channel string
	Err     error
}

func applescriptQuote(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func notifyDesktop(ctx context.Context, cfg config.Config, title, message string) error {
	if _, err := exec.LookPath("osascript"); err != nil {
		return fmt.Errorf("desktop notifications require macOS (osascript not found)")
	}
	script := fmt.Sprintf("display notification %s with title %s",
		applescriptQuote(message), applescriptQuote(title))
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("osascript failed: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func notifyNtfy(ctx context.Context, cfg config.Config, title, message string) error {
	if cfg.Ntfy.Topic == "" {
		return fmt.Errorf("ntfy topic not configured; run `gtclass notify config`")
	}
	url := strings.TrimRight(cfg.Ntfy.Server, "/") + "/" + cfg.Ntfy.Topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("ntfy post failed: %w", err)
	}
	req.Header.Set("Title", title)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy post failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy post failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

type channelFunc func(ctx context.Context, cfg config.Config, title, message string) error

var channels = map[string]channelFunc{
	"desktop": notifyDesktop,
	"ntfy":    notifyNtfy,
}

// AvailableChannels returns the known channel names, sorted.
func AvailableChannels() []string {
	names := make([]string, 0, len(channels))
	for name := range channels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Send dispatches to each requested channel, collecting one Result per
// channel rather than stopping at the first failure.
func Send(ctx context.Context, cfg config.Config, requested []string, title, message string) []Result {
	results := make([]Result, 0, len(requested))
	for _, channel := range requested {
		handler, ok := channels[channel]
		if !ok {
			results = append(results, Result{Channel: channel, Err: fmt.Errorf("unknown channel %q", channel)})
			continue
		}
		err := handler(ctx, cfg, title, message)
		results = append(results, Result{Channel: channel, Err: err})
	}
	return results
}
