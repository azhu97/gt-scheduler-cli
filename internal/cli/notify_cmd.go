package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/notify"
)

func newNotifyCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Configure and test notification channels.",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "test",
		Short: "Send a test notification through every configured channel.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyTest(cmd.Context(), stdout)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "config",
		Short: "Interactively choose notify channels and set up ntfy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyConfig(cmd.InOrStdin(), stdout)
		},
	})
	return cmd
}

func runNotifyTest(ctx context.Context, stdout io.Writer) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	if len(cfg.NotifyChannels) == 0 {
		return fmt.Errorf("no notify channels configured; run `gtclass notify config`")
	}
	results := notify.Send(ctx, cfg, cfg.NotifyChannels, "gtclass test", "This is a test notification.")
	for _, r := range results {
		if r.Err != nil {
			printRed(stdout, "FAILED %s: %s", r.Channel, r.Err)
		} else {
			printGreen(stdout, "OK %s", r.Channel)
		}
	}
	return nil
}

func promptWithDefault(r *bufio.Reader, w io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

func runNotifyConfig(stdin io.Reader, stdout io.Writer) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	r := bufio.NewReader(stdin)

	available := notify.AvailableChannels()
	fmt.Fprintf(stdout, "Available channels: %s\n", strings.Join(available, ", "))
	defaultChannels := strings.Join(cfg.NotifyChannels, ",")
	raw, err := promptWithDefault(r, stdout, "Channels to enable (comma-separated)", defaultChannels)
	if err != nil {
		return err
	}

	var channels []string
	for _, c := range strings.Split(raw, ",") {
		if c = strings.TrimSpace(c); c != "" {
			channels = append(channels, c)
		}
	}

	availableSet := make(map[string]bool, len(available))
	for _, a := range available {
		availableSet[a] = true
	}
	for _, c := range channels {
		if !availableSet[c] {
			return fmt.Errorf("unknown channel %q; available: %s", c, strings.Join(available, ", "))
		}
	}

	for _, c := range channels {
		if c != "ntfy" {
			continue
		}
		topic, err := promptWithDefault(r, stdout, "ntfy topic", cfg.Ntfy.Topic)
		if err != nil {
			return err
		}
		server, err := promptWithDefault(r, stdout, "ntfy server", cfg.Ntfy.Server)
		if err != nil {
			return err
		}
		cfg.Ntfy.Topic = topic
		cfg.Ntfy.Server = server
		break
	}

	cfg.NotifyChannels = channels
	if err := config.SaveConfig(cfg); err != nil {
		return err
	}
	printGreen(stdout, "Saved.")
	return nil
}
