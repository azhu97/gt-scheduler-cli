// Command gtclass is a CLI to browse Georgia Tech course data, track live
// seat/waitlist status for specific CRNs, and notify on changes.
package main

import (
	"fmt"
	"os"

	"github.com/azhu97/gt-scheduler-cli/internal/cli"
	"github.com/azhu97/gt-scheduler-cli/internal/shell"
)

func main() {
	// A bare invocation (no subcommand, no flags — not even --help) drops
	// into the interactive shell, mirroring Click's
	// invoke_without_command=True group callback. Any other invocation
	// (including `gtclass --help`) goes through the normal cobra tree;
	// --help is handled by cobra itself before any RunE runs, so it works
	// without special-casing here.
	if len(os.Args) == 1 {
		if err := shell.RunREPL(os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		return
	}

	root := cli.NewRootCmd(os.Stdout, os.Stderr)
	root.SetArgs(os.Args[1:])
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
