package cli

// End-to-end coverage the Python original never had: no test in the
// Python suite drove cli.py through Click's CliRunner or mocked HTTP, so
// this is new ground, not a port. It runs the real cobra tree against an
// httptest server standing in for GT Scheduler, with a scratch SQLite DB
// and config dir, and asserts on rendered stdout.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
)

const testCatalogJSON = `{
  "courses": {
    "CS 1332": [
      "Data Structures",
      {"A": ["12345", [[0, "MW", "Room 1", 0, ["Jane Doe"], 0, 0, 0]], 3, 0, 0, [], 0]},
      [],
      "desc"
    ]
  },
  "caches": {"periods": ["9:30 am - 10:45 am"]},
  "updatedAt": "2024-01-01T00:00:00Z"
}`

func withScratchEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
}

func withTestCatalogServer(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"terms": [{"term": "202608", "finalized": true}]}`))
	})
	mux.HandleFunc("/202608.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testCatalogJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	prevIndex, prevTerm := gtdata.IndexURL, gtdata.TermURLTmpl
	gtdata.IndexURL = srv.URL + "/index.json"
	gtdata.TermURLTmpl = srv.URL + "/%s.json"
	t.Cleanup(func() {
		gtdata.IndexURL = prevIndex
		gtdata.TermURLTmpl = prevTerm
	})
}

func execCommand(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRootCmd(&out, &errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func TestSearchEndToEnd(t *testing.T) {
	withScratchEnv(t)
	withTestCatalogServer(t)

	stdout, _, err := execCommand(t, "search", "CS 1332")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !strings.Contains(stdout, "12345") {
		t.Errorf("stdout missing CRN 12345:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Data Structures") {
		t.Errorf("stdout missing course title:\n%s", stdout)
	}
}

func TestWatchAddListRemoveEndToEnd(t *testing.T) {
	withScratchEnv(t)
	withTestCatalogServer(t)

	if _, _, err := execCommand(t, "watch", "add", "12345"); err != nil {
		t.Fatalf("watch add failed: %v", err)
	}

	stdout, _, err := execCommand(t, "watch", "list", "--no-refresh")
	if err != nil {
		t.Fatalf("watch list failed: %v", err)
	}
	if !strings.Contains(stdout, "12345") {
		t.Errorf("watch list stdout missing CRN 12345:\n%s", stdout)
	}
	if !strings.Contains(stdout, "CS 1332") {
		t.Errorf("watch list stdout missing course label:\n%s", stdout)
	}

	stdout, _, err = execCommand(t, "watch", "remove", "12345")
	if err != nil {
		t.Fatalf("watch remove failed: %v", err)
	}
	if !strings.Contains(stdout, "Stopped watching") {
		t.Errorf("watch remove stdout unexpected:\n%s", stdout)
	}

	stdout, _, err = execCommand(t, "watch", "list", "--no-refresh")
	if err != nil {
		t.Fatalf("watch list (after remove) failed: %v", err)
	}
	if !strings.Contains(stdout, "empty") {
		t.Errorf("expected empty watchlist after remove:\n%s", stdout)
	}
}

func TestSearchUnknownCourseReturnsEmptyResults(t *testing.T) {
	withScratchEnv(t)
	withTestCatalogServer(t)

	stdout, _, err := execCommand(t, "search", "PHIL 9999")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !strings.Contains(stdout, "No courses found") {
		t.Errorf("expected no-results message:\n%s", stdout)
	}
}

func TestInfoUnknownCRNReturnsError(t *testing.T) {
	withScratchEnv(t)
	withTestCatalogServer(t)

	_, _, err := execCommand(t, "info", "99999999")
	if err == nil {
		t.Fatal("expected an error for an unknown CRN")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to mention 'not found'", err.Error())
	}
}
