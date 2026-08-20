// Package gtdata fetches and parses GT Scheduler's bulk term catalog.
//
// GT Scheduler (https://github.com/gt-scheduler/crawler) publishes a
// heavily-packed JSON blob per term: `courses` maps "SUBJ NUMBER" to
// `[title, sections, prereqs, description, ...]`, where `sections` maps a
// section id to `[crn, meetings, credits, scheduleTypeIdx, campusIdx,
// attributeIdxs, gradeBaseIdx]`, and each meeting is `[periodIdx, days,
// room, scheduleTypeIdx, instructors, campusIdx, dateRangeIdx,
// finalDateIdx]`. `periodIdx` indexes into `caches.periods` to get a human
// time range (e.g. "9:30 am - 10:45 am"). `prereqs` is a nested boolean
// expression tree: `[]` means no prerequisites, otherwise it's
// `["and"|"or", child, child, ...]` where each child is either another such
// list or a leaf `{"id": "SUBJ NUMBER", "grade": "C"}`. No seat data lives
// here — that only comes from the live per-CRN endpoint (see internal/live).
package gtdata

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// IndexURL and TermURLTmpl are vars (not consts) so tests can point them at
// an httptest server instead of the real GT Scheduler endpoints.
var (
	IndexURL    = "https://gt-scheduler.github.io/crawler-v2/index.json"
	TermURLTmpl = "https://gt-scheduler.github.io/crawler-v2/%s.json"
)

// StaleAfter: how long a synced term catalog is considered fresh before
// a command will re-fetch it. Finished terms don't change, so in
// practice this only ever causes re-fetches for the current term.
const StaleAfter = 25 * time.Minute

var seasonNames = map[string]string{"02": "Spring", "05": "Summer", "08": "Fall"}

// TermLabel returns a human label for a term code, e.g. "202302" -> "Spring 2023".
func TermLabel(term string) string {
	if len(term) < 6 {
		return term
	}
	year, season := term[:4], term[4:6]
	name, ok := seasonNames[season]
	if !ok {
		name = season
	}
	return fmt.Sprintf("%s %s", name, year)
}

type userAgentTransport struct{ base http.RoundTripper }

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "gtclass-cli")
	return t.base.RoundTrip(req)
}

// NewClient returns an HTTP client configured like the Python version:
// a 20s timeout and a "gtclass-cli" User-Agent on every request.
func NewClient() *http.Client {
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: userAgentTransport{base: http.DefaultTransport},
	}
}

func doGet(ctx context.Context, client *http.Client, rawURL string, params url.Values) ([]byte, error) {
	if params != nil {
		rawURL = rawURL + "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	return body, nil
}

type termIndexEntry struct {
	Term string `json:"term"`
}

type termIndexResponse struct {
	Terms []json.RawMessage `json:"terms"`
}

// FetchTermIndex returns the list of term codes GT Scheduler knows about,
// newest-and-oldest mixed (caller sorts). crawler-v2's index.json lists
// terms as {"term": ..., "finalized": ...} objects rather than bare
// strings (the old crawler repo's format); both are accepted here.
func FetchTermIndex(ctx context.Context, client *http.Client) ([]string, error) {
	body, err := doGet(ctx, client, IndexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch term index: %w", err)
	}
	var parsed termIndexResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to fetch term index: %w", err)
	}

	terms := make([]string, 0, len(parsed.Terms))
	for _, raw := range parsed.Terms {
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			terms = append(terms, asString)
			continue
		}
		var entry termIndexEntry
		if err := json.Unmarshal(raw, &entry); err == nil {
			terms = append(terms, entry.Term)
		}
	}
	return terms, nil
}

// FetchTermCatalog fetches the raw (still packed) catalog blob for term.
func FetchTermCatalog(ctx context.Context, client *http.Client, term string) (map[string]any, error) {
	body, err := doGet(ctx, client, fmt.Sprintf(TermURLTmpl, term), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch catalog for term %s: %w", term, err)
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to fetch catalog for term %s: %w", term, err)
	}
	return data, nil
}

func cleanDays(days string) string {
	return strings.TrimSpace(strings.ReplaceAll(days, "&nbsp;", ""))
}

// asString formats a loosely-typed JSON scalar (string or number) as a
// string the way Python's str(x) would for the CRN/section-id fields.
func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func asInt(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func asFloatPtr(v any) *float64 {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	return &f
}

func splitCourseKey(key string) (subject, courseNumber string) {
	subject, courseNumber, _ = strings.Cut(key, " ")
	return subject, courseNumber
}

// CourseRow is one flattened (term, crn) row of the `courses` table.
type CourseRow struct {
	Term         string
	CRN          string
	Subject      string
	CourseNumber string
	Section      string
	Title        string
	Instructor   string
	MeetingDays  string
	MeetingTime  string
	Location     string
	Credits      *float64
}

// ParseTermCatalog flattens the packed catalog blob into one row per (term, crn).
func ParseTermCatalog(data map[string]any, term string) []CourseRow {
	caches, _ := data["caches"].(map[string]any)
	periodsRaw, _ := caches["periods"].([]any)
	periods := make([]string, len(periodsRaw))
	for i, p := range periodsRaw {
		if s, ok := p.(string); ok {
			periods[i] = s
		}
	}

	var rows []CourseRow
	courses, _ := data["courses"].(map[string]any)
	for courseKey, courseValRaw := range courses {
		courseVal, ok := courseValRaw.([]any)
		if !ok || len(courseVal) < 2 {
			continue
		}
		title, _ := courseVal[0].(string)
		sections, _ := courseVal[1].(map[string]any)
		subject, courseNumber := splitCourseKey(courseKey)

		for sectionID, sectionValRaw := range sections {
			sectionVal, ok := sectionValRaw.([]any)
			if !ok || len(sectionVal) < 2 {
				continue
			}
			crn := asString(sectionVal[0])
			meetings, _ := sectionVal[1].([]any)
			var credits *float64
			if len(sectionVal) > 2 {
				credits = asFloatPtr(sectionVal[2])
			}

			var daysParts, timeParts, locationParts, instructors []string

			for _, mRaw := range meetings {
				m, ok := mRaw.([]any)
				if !ok || len(m) < 5 {
					continue
				}
				days, _ := m[1].(string)
				room, _ := m[2].(string)
				meetingInstructors, _ := m[4].([]any)

				daysClean := cleanDays(days)
				if daysClean == "" {
					daysClean = "TBA"
				}
				timeStr := "TBA"
				if idx, ok := asInt(m[0]); ok && idx >= 0 && idx < len(periods) {
					timeStr = periods[idx]
				}
				daysParts = append(daysParts, daysClean)
				timeParts = append(timeParts, timeStr)
				if room == "" {
					room = "TBA"
				}
				locationParts = append(locationParts, room)

				for _, instrRaw := range meetingInstructors {
					instr, _ := instrRaw.(string)
					if instr == "" {
						continue
					}
					found := false
					for _, existing := range instructors {
						if existing == instr {
							found = true
							break
						}
					}
					if !found {
						instructors = append(instructors, instr)
					}
				}
			}

			rows = append(rows, CourseRow{
				Term:         term,
				CRN:          crn,
				Subject:      subject,
				CourseNumber: courseNumber,
				Section:      sectionID,
				Title:        title,
				Instructor:   strings.Join(instructors, ", "),
				MeetingDays:  strings.Join(daysParts, "; "),
				MeetingTime:  strings.Join(timeParts, "; "),
				Location:     strings.Join(locationParts, "; "),
				Credits:      credits,
			})
		}
	}
	return rows
}

// PrereqRow is one row of the `course_prereqs` table.
type PrereqRow struct {
	Term         string
	Subject      string
	CourseNumber string
	Title        string
	PrereqsJSON  string
}

// ParseTermPrereqs extracts the course-level prerequisite tree for every course in the catalog.
func ParseTermPrereqs(data map[string]any, term string) []PrereqRow {
	var rows []PrereqRow
	courses, _ := data["courses"].(map[string]any)
	for courseKey, courseValRaw := range courses {
		courseVal, ok := courseValRaw.([]any)
		if !ok || len(courseVal) < 1 {
			continue
		}
		title, _ := courseVal[0].(string)
		var prereqs any = []any{}
		if len(courseVal) > 2 {
			if arr, ok := courseVal[2].([]any); ok && len(arr) > 0 {
				prereqs = arr
			}
		}
		subject, courseNumber := splitCourseKey(courseKey)

		prereqsJSON, err := json.Marshal(prereqs)
		if err != nil {
			prereqsJSON = []byte("[]")
		}
		rows = append(rows, PrereqRow{
			Term:         term,
			Subject:      subject,
			CourseNumber: courseNumber,
			Title:        title,
			PrereqsJSON:  string(prereqsJSON),
		})
	}
	return rows
}

type SyncResult struct {
	Synced      bool
	CourseCount int
}

func hasPrereqs(db *sql.DB, term string) bool {
	var one int
	err := db.QueryRow("SELECT 1 FROM course_prereqs WHERE term = ? LIMIT 1", term).Scan(&one)
	return err == nil
}

// SyncTerm ensures term's catalog is cached in SQLite, fetching if stale/missing.
func SyncTerm(ctx context.Context, db *sql.DB, client *http.Client, term string, force bool) (SyncResult, error) {
	var syncedAt string
	var courseCount int
	err := db.QueryRow("SELECT synced_at, course_count FROM terms_meta WHERE term = ?", term).Scan(&syncedAt, &courseCount)
	if err == nil && !force {
		t, parseErr := time.Parse(time.RFC3339, syncedAt)
		fresh := parseErr == nil && time.Since(t) < StaleAfter
		// A term synced before course_prereqs existed (or synced while empty)
		// won't have prereq rows yet; re-fetch once to backfill them even if
		// otherwise "fresh", rather than waiting out StaleAfter.
		backfilled := courseCount == 0 || hasPrereqs(db, term)
		if fresh && backfilled {
			return SyncResult{Synced: false, CourseCount: courseCount}, nil
		}
	}

	data, err := FetchTermCatalog(ctx, client, term)
	if err != nil {
		return SyncResult{}, err
	}
	rows := ParseTermCatalog(data, term)
	prereqRows := ParseTermPrereqs(data, term)
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return SyncResult{}, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM courses WHERE term = ?", term); err != nil {
		return SyncResult{}, err
	}
	insertCourse, err := tx.Prepare(`
		INSERT INTO courses
			(term, crn, subject, course_number, section, title,
			 instructor, meeting_days, meeting_time, location, credits)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return SyncResult{}, err
	}
	for _, r := range rows {
		if _, err := insertCourse.Exec(r.Term, r.CRN, r.Subject, r.CourseNumber, r.Section, r.Title,
			r.Instructor, r.MeetingDays, r.MeetingTime, r.Location, r.Credits); err != nil {
			insertCourse.Close()
			return SyncResult{}, err
		}
	}
	insertCourse.Close()

	if _, err := tx.Exec("DELETE FROM course_prereqs WHERE term = ?", term); err != nil {
		return SyncResult{}, err
	}
	insertPrereq, err := tx.Prepare(`
		INSERT INTO course_prereqs (term, subject, course_number, title, prereqs_json)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return SyncResult{}, err
	}
	for _, r := range prereqRows {
		if _, err := insertPrereq.Exec(r.Term, r.Subject, r.CourseNumber, r.Title, r.PrereqsJSON); err != nil {
			insertPrereq.Close()
			return SyncResult{}, err
		}
	}
	insertPrereq.Close()

	var upstreamUpdatedAt any
	if v, ok := data["updatedAt"]; ok {
		upstreamUpdatedAt = v
	}
	if _, err := tx.Exec(`
		INSERT INTO terms_meta (term, upstream_updated_at, synced_at, course_count)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (term) DO UPDATE SET
			upstream_updated_at = excluded.upstream_updated_at,
			synced_at = excluded.synced_at,
			course_count = excluded.course_count
	`, term, upstreamUpdatedAt, now, len(rows)); err != nil {
		return SyncResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Synced: true, CourseCount: len(rows)}, nil
}

// ResolveDefaultTerm picks the current term: the latest one GT Scheduler
// actually has data for.
//
// GT Scheduler's index sometimes lists terms ahead of what its crawler has
// populated yet (an empty {"courses": {}} placeholder), so this walks the
// index newest-first and syncs each candidate until one has course data,
// rather than trusting the max term code blindly. Falls back to the most
// recently synced non-empty local term if the index can't be reached (e.g.
// offline), and finally errors if nothing is cached.
func ResolveDefaultTerm(ctx context.Context, db *sql.DB, client *http.Client) (string, error) {
	if client != nil {
		terms, err := FetchTermIndex(ctx, client)
		if err == nil {
			sort.Sort(sort.Reverse(sort.StringSlice(terms)))
			for _, term := range terms {
				result, err := SyncTerm(ctx, db, client, term, false)
				if err != nil {
					continue
				}
				if result.CourseCount > 0 {
					return term, nil
				}
			}
		}
	}

	var term string
	err := db.QueryRow(
		"SELECT term FROM terms_meta WHERE course_count > 0 ORDER BY term DESC LIMIT 1",
	).Scan(&term)
	if err == nil {
		return term, nil
	}

	return "", fmt.Errorf(
		"couldn't find a term with course data (GT Scheduler's crawler may " +
			"be behind for recent terms); pass --term explicitly",
	)
}
