package formatting

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/azhu97/gt-scheduler-cli/internal/dbstore"
)

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

func insertPrereq(t *testing.T, db *sql.DB, term, subject, number, title string, prereqs any) {
	t.Helper()
	data, err := json.Marshal(prereqs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO course_prereqs (term, subject, course_number, title, prereqs_json) VALUES (?, ?, ?, ?, ?)",
		term, subject, number, title, string(data),
	); err != nil {
		t.Fatal(err)
	}
}

func render(t *testing.T, db *sql.DB, term, subject, number string, directOnly bool) string {
	t.Helper()
	var buf bytes.Buffer
	if err := PrintPrereqTree(&buf, db, term, subject, number, directOnly); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestPrintPrereqTreeNoPrereqs(t *testing.T) {
	db := newTestDB(t)
	insertPrereq(t, db, "202302", "CS", "1301", "Intro Computing", []any{})

	out := render(t, db, "202302", "CS", "1301", false)
	if !strings.Contains(out, "CS 1301") {
		t.Errorf("output missing CS 1301: %s", out)
	}
	if !strings.Contains(out, "no prerequisites") {
		t.Errorf("output missing 'no prerequisites': %s", out)
	}
}

func TestPrintPrereqTreeRecursesIntoNestedCourse(t *testing.T) {
	db := newTestDB(t)
	insertPrereq(t, db, "202302", "CS", "1332", "Data Structures",
		[]any{"or", map[string]any{"id": "CS 1331", "grade": "C"}})
	insertPrereq(t, db, "202302", "CS", "1331", "Intro OOP", []any{})

	out := render(t, db, "202302", "CS", "1332", false)
	if !strings.Contains(out, "CS 1332") || !strings.Contains(out, "CS 1331") {
		t.Errorf("output missing expected courses: %s", out)
	}
	if !strings.Contains(out, "min grade C") {
		t.Errorf("output missing grade annotation: %s", out)
	}
}

func TestPrintPrereqTreeHandlesUnresolvableReference(t *testing.T) {
	db := newTestDB(t)
	insertPrereq(t, db, "202302", "CS", "3510", "Algorithms",
		[]any{"or", map[string]any{"id": "MATH 3012", "grade": "D"}})

	out := render(t, db, "202302", "CS", "3510", false)
	if !strings.Contains(out, "MATH 3012") {
		t.Errorf("output missing MATH 3012: %s", out)
	}
	if !strings.Contains(out, "no prerequisite data") {
		t.Errorf("output missing 'no prerequisite data': %s", out)
	}
}

func TestPrintPrereqTreeDirectOnlySkipsGrandchildren(t *testing.T) {
	db := newTestDB(t)
	insertPrereq(t, db, "202302", "CS", "1332", "Data Structures",
		[]any{"or", map[string]any{"id": "CS 1331", "grade": "C"}})
	insertPrereq(t, db, "202302", "CS", "1331", "Intro OOP",
		[]any{"or", map[string]any{"id": "CS 1301", "grade": "C"}})
	insertPrereq(t, db, "202302", "CS", "1301", "Intro Computing", []any{})

	out := render(t, db, "202302", "CS", "1332", true)
	if !strings.Contains(out, "CS 1332") || !strings.Contains(out, "CS 1331") {
		t.Errorf("output missing expected courses: %s", out)
	}
	if strings.Contains(out, "CS 1301") {
		t.Errorf("direct-only mode should not expand CS 1331's own prerequisites: %s", out)
	}
}

func TestPrintPrereqTreeAvoidsInfiniteCycle(t *testing.T) {
	db := newTestDB(t)
	insertPrereq(t, db, "202302", "CS", "A", "Course A", []any{"or", map[string]any{"id": "CS B"}})
	insertPrereq(t, db, "202302", "CS", "B", "Course B", []any{"or", map[string]any{"id": "CS A"}})

	out := render(t, db, "202302", "CS", "A", false)
	if !strings.Contains(out, "already shown above") {
		t.Errorf("output missing cycle guard message: %s", out)
	}
}
