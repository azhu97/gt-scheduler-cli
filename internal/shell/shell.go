// Package shell implements the interactive shell: bare `gtclass` (no
// subcommand) drops into this.
//
// Reuses the cobra command tree from internal/cli as the parser/dispatcher
// — each typed line is split and fed through a *freshly built* root
// command, so every command (search, watch add, daemon start, ...) gets
// shell support for free with no duplicated logic. The tree is rebuilt
// per line (rather than reused) because cobra's pflag.FlagSet doesn't
// reset "Changed" state between Execute() calls — see the package doc in
// internal/cli/common.go.
//
// On startup, the shell asks which semester to browse (current vs. a
// specific previous one) and stores the choice in
// config.SessionTermEnvVar for the rest of the process — cli.resolveTerm
// checks that env var. This is the one piece of session state the shell
// carries; a --term flag on any single command still overrides it, and
// config.toml's default_term is unaffected (the choice is never persisted
// to disk).
package shell

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/google/shlex"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/azhu97/gt-scheduler-cli/internal/cli"
	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/dbstore"
	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
)

var exitCommands = map[string]bool{"exit": true, "quit": true}

func promptChoice(r *bufio.Reader, w io.Writer, prompt string, valid map[string]bool, def string) (string, bool) {
	suffix := ""
	if def != "" {
		suffix = fmt.Sprintf(" [%s]", def)
	}
	for {
		fmt.Fprintf(w, "%s%s: ", prompt, suffix)
		line, err := r.ReadString('\n')
		if err != nil {
			fmt.Fprintln(w)
			return "", false
		}
		raw := strings.TrimSpace(line)
		if raw == "" && def != "" {
			return def, true
		}
		if valid[raw] {
			return raw, true
		}
		fmt.Fprintf(w, "%s\n", color.RedString("invalid choice: %q", raw))
	}
}

// selectTerm asks current-vs-previous, then which previous semester.
// ok=false means the user cancelled (EOF) — the caller should fall back to
// the bare, term-less prompt.
func selectTerm(ctx context.Context, db *sql.DB, client *http.Client, stdin *bufio.Reader, stdout, stderr io.Writer) (string, bool) {
	current, err := gtdata.ResolveDefaultTerm(ctx, db, client)
	if err != nil {
		fmt.Fprintln(stderr, color.YellowString("%s", err))
		return "", false
	}

	var terms []string
	if idxTerms, err := gtdata.FetchTermIndex(ctx, client); err == nil {
		terms = append(terms, idxTerms...)
		sort.Sort(sort.Reverse(sort.StringSlice(terms)))
	} else if rows, qerr := db.Query("SELECT term FROM terms_meta ORDER BY term DESC"); qerr == nil {
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil {
				terms = append(terms, t)
			}
		}
		rows.Close()
	}

	fmt.Fprintln(stdout, "Which semester do you want to browse?")
	fmt.Fprintf(stdout, "  1) Current (most recent with data): %s [%s]\n", gtdata.TermLabel(current), current)
	fmt.Fprintln(stdout, "  2) A previous semester")
	choice, ok := promptChoice(stdin, stdout, "Enter 1 or 2", map[string]bool{"1": true, "2": true}, "1")
	if !ok {
		return "", false
	}
	if choice == "1" {
		return current, true
	}

	var previous []string
	for _, t := range terms {
		if t != current {
			previous = append(previous, t)
		}
	}
	if len(previous) == 0 {
		fmt.Fprintln(stdout, color.YellowString("No previous semesters available."))
		return current, true
	}

	fmt.Fprintln(stdout, "Previous semesters:")
	validIdx := make(map[string]bool, len(previous))
	for i, t := range previous {
		fmt.Fprintf(stdout, "  %d) %s [%s]\n", i+1, gtdata.TermLabel(t), t)
		validIdx[strconv.Itoa(i+1)] = true
	}
	idx, ok := promptChoice(stdin, stdout, "Enter a number", validIdx, "")
	if !ok {
		return "", false
	}
	n, _ := strconv.Atoi(idx)
	return previous[n-1], true
}

func completerForCmd(cmd *cobra.Command) readline.PrefixCompleterInterface {
	var children []readline.PrefixCompleterInterface
	for _, sub := range cmd.Commands() {
		children = append(children, completerForCmd(sub))
	}
	return readline.PcItem(cmd.Name(), children...)
}

func newCompleter(root *cobra.Command) *readline.PrefixCompleter {
	items := []readline.PrefixCompleterInterface{
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("help"),
	}
	for _, sub := range root.Commands() {
		items = append(items, completerForCmd(sub))
	}
	return readline.NewPrefixCompleter(items...)
}

// RunREPL starts the interactive shell. Returns immediately (printing a
// hint) if stdin isn't a terminal, matching the non-blocking behavior of
// piping into `gtclass` documented in CLAUDE.md.
func RunREPL(stdout, stderr io.Writer) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(stdout, "Not an interactive terminal; run `gtclass --help` for usage.")
		return nil
	}

	ctx := context.Background()

	db, err := dbstore.Connect("")
	if err != nil {
		return err
	}
	if err := dbstore.InitDB(db); err != nil {
		db.Close()
		return err
	}
	client := gtdata.NewClient()

	stdinReader := bufio.NewReader(os.Stdin)
	selectedTerm, ok := selectTerm(ctx, db, client, stdinReader, stdout, stderr)
	db.Close()

	prompt := "gtclass> "
	if ok {
		os.Setenv(config.SessionTermEnvVar, selectedTerm)
		label := gtdata.TermLabel(selectedTerm)
		prompt = fmt.Sprintf("gtclass>%s> ", label)
		fmt.Fprintln(stdout, color.GreenString(
			"Using %s [%s] for this session (pass --term to override for a single command).",
			label, selectedTerm))
	}

	fmt.Fprintln(stdout, "Type a command (e.g. `search CS 4210`), or `exit`/Ctrl+D to quit.")

	rl, err := readline.NewEx(&readline.Config{
		Prompt:       prompt,
		AutoComplete: newCompleter(cli.NewRootCmd(io.Discard, io.Discard)),
		Stdout:       stdout,
		Stderr:       stderr,
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err == readline.ErrInterrupt {
			continue
		}
		if err == io.EOF {
			fmt.Fprintln(stdout)
			break
		}
		if err != nil {
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if exitCommands[line] {
			break
		}
		if line == "help" {
			line = "--help"
		}

		args, err := shlex.Split(line)
		if err != nil {
			fmt.Fprintln(stderr, color.RedString("parse error: %s", err))
			continue
		}

		root := cli.NewRootCmd(stdout, stderr)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			fmt.Fprintln(stderr, color.RedString("error: %s", err))
		}
	}
	return nil
}
