package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azhu97/gt-scheduler-cli/internal/daemon"
	"github.com/azhu97/gt-scheduler-cli/internal/poller"
)

func newDaemonCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the background poller.",
	}
	cmd.AddCommand(newDaemonStartCmd(stdout))
	cmd.AddCommand(newDaemonStopCmd(stdout))
	cmd.AddCommand(newDaemonStatusCmd(stdout))
	cmd.AddCommand(newDaemonInstallCmd(stdout))
	cmd.AddCommand(newDaemonUninstallCmd(stdout))
	return cmd
}

func newDaemonStartCmd(stdout io.Writer) *cobra.Command {
	var interval int
	var foreground bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the poller (detaches into the background by default).",
		RunE: func(cmd *cobra.Command, args []string) error {
			var intervalPtr *int
			if cmd.Flags().Changed("interval") {
				intervalPtr = &interval
			}
			return runDaemonStart(cmd.Context(), stdout, intervalPtr, foreground)
		},
	}
	cmd.Flags().IntVar(&interval, "interval", 0, "Poll interval in seconds.")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in this terminal instead of detaching.")
	return cmd
}

func runDaemonStart(ctx context.Context, stdout io.Writer, interval *int, foreground bool) error {
	running, pid := daemon.Status()
	if running {
		return fmt.Errorf("daemon already running (pid %d)", pid)
	}

	if foreground {
		fmt.Fprintln(stdout, "Starting poller in foreground (Ctrl+C to stop)...")
		onTick := func(events []poller.PollEvent, errs []poller.PollError) {
			ts := time.Now().UTC().Format(time.RFC3339)
			fmt.Fprintf(stdout, "%s polled %d CRN(s), %d error(s)\n", ts, len(events), len(errs))
			for _, e := range events {
				if len(e.Changes) > 0 {
					fmt.Fprintf(stdout, "  CRN %s: %s\n", e.CRN, strings.Join(e.Changes, "; "))
				}
			}
			for _, e := range errs {
				fmt.Fprintf(stdout, "  warning: CRN %s: %s\n", e.CRN, e.Error)
			}
		}
		return daemon.RunForeground(ctx, interval, onTick)
	}

	startedPID, err := daemon.StartDetached(interval)
	if err != nil {
		return err
	}
	printGreen(stdout, "Started gtclass daemon (pid %d)", startedPID)
	return nil
}

func newDaemonStopCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the background poller.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStop(stdout)
		},
	}
}

func runDaemonStop(stdout io.Writer) error {
	if !daemon.Stop() {
		return fmt.Errorf("daemon is not running")
	}
	printGreen(stdout, "Stopped gtclass daemon")
	if daemon.IsLaunchdInstalled() {
		printDim(stdout, "note: still installed via launchd — it will restart at the "+
			"next login. Run `gtclass daemon uninstall` to disable that.")
	}
	return nil
}

func newDaemonStatusCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the background poller is running, and its poll stats.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStatus(stdout)
		},
	}
}

func runDaemonStatus(stdout io.Writer) error {
	info := daemon.StatusDetail()
	launchdLine := "  launchd:       not installed"
	if daemon.IsLaunchdInstalled() {
		launchdLine = "  launchd:       installed (auto-restarts on crash/reboot)"
	}

	if !info.Running {
		printYellow(stdout, "not running")
		fmt.Fprintln(stdout, launchdLine)
		return nil
	}

	printGreen(stdout, "running (pid %d)", info.PID)
	if info.PollInterval != 0 {
		fmt.Fprintf(stdout, "  poll interval: %ds\n", info.PollInterval)
	} else {
		fmt.Fprintln(stdout, "  poll interval: unknown")
	}
	startedAt := info.StartedAt
	if startedAt == "" {
		startedAt = "unknown"
	}
	fmt.Fprintf(stdout, "  started at:    %s\n", startedAt)
	if info.LastPolledAt != "" {
		fmt.Fprintf(stdout, "  last polled:   %s (%d CRN(s), %d error(s))\n",
			info.LastPolledAt, info.LastPollCRNs, info.LastPollErrors)
	} else {
		fmt.Fprintln(stdout, "  last polled:   never (first poll pending)")
	}
	if info.LastError != "" {
		printRed(stdout, "  last error:   %s (at %s)", info.LastError, info.LastErrorAt)
	}
	fmt.Fprintln(stdout, launchdLine)
	return nil
}

func newDaemonInstallCmd(stdout io.Writer) *cobra.Command {
	var interval int
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install as a launchd LaunchAgent: auto-starts at login, restarts on crash.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var intervalPtr *int
			if cmd.Flags().Changed("interval") {
				intervalPtr = &interval
			}
			path, err := daemon.InstallLaunchd(intervalPtr)
			if err != nil {
				return fmt.Errorf("failed to install launchd agent: %w", err)
			}
			printGreen(stdout, "Installed launchd agent -> %s", path)
			fmt.Fprintln(stdout, "It will start automatically at login and restart if it crashes.")
			return nil
		},
	}
	cmd.Flags().IntVar(&interval, "interval", 0, "Poll interval in seconds.")
	return cmd
}

func newDaemonUninstallCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the launchd LaunchAgent (stops it and disables auto-start).",
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := daemon.UninstallLaunchd()
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("launchd agent is not installed")
			}
			printGreen(stdout, "Uninstalled launchd agent")
			return nil
		},
	}
}
