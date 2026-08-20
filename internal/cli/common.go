// Package cli wires the cobra command tree: search / info / tree / watch
// add|list|remove / notify test|config / daemon start|stop|status|install|uninstall.
//
// Each command opens its own DB connection and HTTP client and closes them
// before returning (matching the Python original's `with db.connect() as
// conn, gtdata.new_client() as client:` per command) rather than sharing a
// long-lived connection — that's what makes NewRootCmd cheap enough to call
// fresh on every line of the interactive shell (see internal/shell), which
// is how this port resolves cobra's flag-state-doesn't-reset-between-
// Execute()-calls problem for a REPL.
package cli

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/dbstore"
	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
)

func printGreen(w io.Writer, format string, args ...any) {
	color.New(color.FgGreen).Fprintf(w, format+"\n", args...)
}

func printYellow(w io.Writer, format string, args ...any) {
	color.New(color.FgYellow).Fprintf(w, format+"\n", args...)
}

func printRed(w io.Writer, format string, args ...any) {
	color.New(color.FgRed).Fprintf(w, format+"\n", args...)
}

func printDim(w io.Writer, format string, args ...any) {
	color.New(color.Faint).Fprintf(w, format+"\n", args...)
}

var courseKeyRE = regexp.MustCompile(`^\s*([A-Za-z]+)\s*(\d[\dA-Za-z]*)\s*$`)

// splitCourseQuery parses a "SUBJECT NUMBER" query like "CS 4210" or "cs4210".
func splitCourseQuery(query string) (subject, number string, ok bool) {
	m := courseKeyRE.FindStringSubmatch(query)
	if m == nil {
		return "", "", false
	}
	return strings.ToUpper(m[1]), strings.ToUpper(m[2]), true
}

// resolveTerm implements the term resolution precedence documented in
// CLAUDE.md: --term flag > shell session term (env var) > config.toml
// default_term > latest non-empty term from GT Scheduler's index.
func resolveTerm(ctx context.Context, db *sql.DB, client *http.Client, termFlag string, cfg config.Config) (string, error) {
	if termFlag != "" {
		return termFlag, nil
	}
	if session := os.Getenv(config.SessionTermEnvVar); session != "" {
		return session, nil
	}
	if cfg.DefaultTerm != "" {
		return cfg.DefaultTerm, nil
	}
	return gtdata.ResolveDefaultTerm(ctx, db, client)
}

func syncTerm(ctx context.Context, db *sql.DB, client *http.Client, term string) error {
	_, err := gtdata.SyncTerm(ctx, db, client, term, false)
	return err
}

// openDB opens (and initializes the schema on) the default database.
func openDB() (*sql.DB, error) {
	db, err := dbstore.Connect("")
	if err != nil {
		return nil, err
	}
	if err := dbstore.InitDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
