package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRootCmd builds a fresh cobra command tree writing to stdout/stderr.
// Called once for a one-shot CLI invocation, and fresh on every line typed
// into the interactive shell (internal/shell) — see the package doc in
// common.go for why a fresh tree per call, rather than resetting flag
// state, is the chosen fix for cobra's REPL-reuse gotcha.
func NewRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "gtclass",
		Short:         "Browse GT course data and watch CRNs for seat/waitlist changes.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	root.AddCommand(newSearchCmd(stdout, stderr))
	root.AddCommand(newInfoCmd(stdout, stderr))
	root.AddCommand(newTreeCmd(stdout, stderr))
	root.AddCommand(newWatchCmd(stdout, stderr))
	root.AddCommand(newNotifyCmd(stdout, stderr))
	root.AddCommand(newDaemonCmd(stdout, stderr))

	return root
}
