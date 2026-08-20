package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/formatting"
	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
)

func newTreeCmd(stdout, stderr io.Writer) *cobra.Command {
	var term string
	var min bool
	cmd := &cobra.Command{
		Use:   "tree QUERY",
		Short: `Show a tree of prerequisites for a course (e.g. "CS 3510").`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTree(cmd.Context(), stdout, args[0], term, min)
		},
	}
	cmd.Flags().StringVar(&term, "term", "", "Term code. Defaults to the current term.")
	cmd.Flags().BoolVar(&min, "min", false, "Only show direct prerequisites, without expanding their own prerequisites.")
	return cmd
}

func runTree(ctx context.Context, stdout io.Writer, query, term string, directOnly bool) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	client := gtdata.NewClient()

	resolvedTerm, err := resolveTerm(ctx, db, client, term, cfg)
	if err != nil {
		return err
	}
	if err := syncTerm(ctx, db, client, resolvedTerm); err != nil {
		return err
	}

	subject, number, ok := splitCourseQuery(query)
	if !ok {
		return fmt.Errorf("couldn't parse %q as \"SUBJECT NUMBER\"", query)
	}

	var one int
	err = db.QueryRow(
		"SELECT 1 FROM course_prereqs WHERE term = ? AND subject = ? AND course_number = ?",
		resolvedTerm, subject, number,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s %s not found in term %s", subject, number, resolvedTerm)
	}
	if err != nil {
		return err
	}

	return formatting.PrintPrereqTree(stdout, db, resolvedTerm, subject, number, directOnly)
}
