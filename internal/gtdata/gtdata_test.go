package gtdata

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/azhu97/gt-scheduler-cli/internal/dbstore"
)

func loadFixture(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/sample_term.json")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParseTermCatalogBasicSection(t *testing.T) {
	rows := ParseTermCatalog(loadFixture(t), "202302")
	byCRN := map[string]CourseRow{}
	for _, r := range rows {
		byCRN[r.CRN] = r
	}

	row, ok := byCRN["25096"]
	if !ok {
		t.Fatal("expected CRN 25096 in parsed rows")
	}
	if row.Subject != "ACCT" || row.CourseNumber != "2101" || row.Section != "A" {
		t.Errorf("unexpected identity: %+v", row)
	}
	if row.Title != "Accounting I" {
		t.Errorf("title = %q", row.Title)
	}
	if row.MeetingDays != "MW" {
		t.Errorf("meeting_days = %q", row.MeetingDays)
	}
	if row.MeetingTime != "12:30 pm - 1:45 pm" {
		t.Errorf("meeting_time = %q", row.MeetingTime)
	}
	if row.Location != "Scheller College of Business 103" {
		t.Errorf("location = %q", row.Location)
	}
	if row.Instructor != "Eric R Condie (P)" {
		t.Errorf("instructor = %q", row.Instructor)
	}
	if row.Credits == nil || *row.Credits != 3.0 {
		t.Errorf("credits = %v", row.Credits)
	}
}

func TestParseTermCatalogHandlesTBASection(t *testing.T) {
	rows := ParseTermCatalog(loadFixture(t), "202302")
	byCRN := map[string]CourseRow{}
	for _, r := range rows {
		byCRN[r.CRN] = r
	}

	tba, ok := byCRN["34617"]
	if !ok {
		t.Fatal("expected CRN 34617")
	}
	if tba.MeetingDays != "TBA" {
		t.Errorf("meeting_days = %q, want TBA", tba.MeetingDays)
	}
	if tba.Location != "TBA" {
		t.Errorf("location = %q, want TBA", tba.Location)
	}

	row, ok := byCRN["25594"]
	if !ok {
		t.Fatal("expected CRN 25594")
	}
	if row.Subject != "CS" || row.CourseNumber != "1331" {
		t.Errorf("unexpected identity: %+v", row)
	}
	if row.Credits == nil || *row.Credits != 3.0 {
		t.Errorf("credits = %v", row.Credits)
	}
}

func TestParseTermCatalogRowCount(t *testing.T) {
	rows := ParseTermCatalog(loadFixture(t), "202302")
	if len(rows) != 11 { // 4 ACCT 2101 sections + 7 CS 1331 sections
		t.Errorf("len(rows) = %d, want 11", len(rows))
	}
	for _, r := range rows {
		if r.Term != "202302" {
			t.Errorf("term = %q, want 202302", r.Term)
		}
	}
}

func TestParseTermPrereqsNoPrereqs(t *testing.T) {
	rows := ParseTermPrereqs(loadFixture(t), "202302")
	for _, r := range rows {
		if r.Subject == "ACCT" && r.CourseNumber == "2101" {
			if r.Title != "Accounting I" {
				t.Errorf("title = %q", r.Title)
			}
			if r.PrereqsJSON != "[]" {
				t.Errorf("prereqs_json = %q, want []", r.PrereqsJSON)
			}
			return
		}
	}
	t.Fatal("expected ACCT 2101 row")
}

func TestParseTermPrereqsNestedExpression(t *testing.T) {
	rows := ParseTermPrereqs(loadFixture(t), "202302")
	for _, r := range rows {
		if r.Subject == "CS" && r.CourseNumber == "1331" {
			if !strings.HasPrefix(r.PrereqsJSON, `["or"`) {
				t.Errorf("prereqs_json = %q, want to start with [\"or\"", r.PrereqsJSON)
			}
			if !strings.Contains(r.PrereqsJSON, `{"grade":"C","id":"CS 1301"}`) &&
				!strings.Contains(r.PrereqsJSON, `{"id":"CS 1301","grade":"C"}`) {
				t.Errorf("prereqs_json missing CS 1301 leaf: %s", r.PrereqsJSON)
			}
			return
		}
	}
	t.Fatal("expected CS 1331 row")
}

// overrideURLs points IndexURL/TermURLTmpl at a test server for the
// duration of a test; call both returned funcs (e.g. via defer) to restore
// the real GT Scheduler endpoints afterward.
func overrideURLs(index, termTmpl string) (restoreIndex, restoreTerm func()) {
	prevIndex, prevTerm := IndexURL, TermURLTmpl
	IndexURL, TermURLTmpl = index, termTmpl
	return func() { IndexURL = prevIndex }, func() { TermURLTmpl = prevTerm }
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := dbstore.Connect(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbstore.InitDB(db); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestResolveDefaultTermSkipsEmptyPlaceholder is the regression test called
// for in the migration plan: GT Scheduler's index.json can list a term
// ahead of what its crawler has actually populated (an empty
// {"courses": {}} catalog). ResolveDefaultTerm must walk past it to the
// next non-empty term rather than trusting the newest index entry blindly.
func TestResolveDefaultTermSkipsEmptyPlaceholder(t *testing.T) {
	fixtureBytes, err := os.ReadFile("testdata/sample_term.json")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"terms": [{"term": "202608", "finalized": false}, {"term": "202302", "finalized": true}]}`))
	})
	mux.HandleFunc("/202608.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"courses": {}}`))
	})
	mux.HandleFunc("/202302.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixtureBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	restoreIndex, restoreTerm := overrideURLs(srv.URL+"/index.json", srv.URL+"/%s.json")
	defer restoreIndex()
	defer restoreTerm()

	db := newTestDB(t)
	term, err := ResolveDefaultTerm(context.Background(), db, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if term != "202302" {
		t.Errorf("term = %q, want 202302 (the empty 202608 placeholder should be skipped)", term)
	}
}

func TestFetchTermIndexAcceptsObjectAndStringEntries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"terms": [{"term": "202608"}, "202302"]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	restoreIndex, restoreTerm := overrideURLs(srv.URL+"/index.json", srv.URL+"/%s.json")
	defer restoreIndex()
	defer restoreTerm()

	terms, err := FetchTermIndex(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 2 || terms[0] != "202608" || terms[1] != "202302" {
		t.Errorf("terms = %v", terms)
	}
}
