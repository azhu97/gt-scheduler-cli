// Package live fetches live per-CRN seat/waitlist data, proxied live from Oscar.
//
// The endpoint returns an HTML fragment (not JSON) meant to be dropped into
// a page, e.g.:
//
//	<span class="status-bold">Enrollment Seats Available:</span>
//	<span dir="ltr">0</span>
//
// so this package scrapes the handful of numbers out of it with regexes
// rather than a full HTML parser.
package live

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// LiveURL is a var (not a const) so tests can point it at an httptest server.
var LiveURL = "https://gt-scheduler.azurewebsites.net/proxy/class_section"

var fieldPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"enrollment_actual", regexp.MustCompile(`Enrollment Actual:</span>\s*<span[^>]*>\s*(\d+)`)},
	{"seats_total", regexp.MustCompile(`Enrollment Maximum:</span>\s*<span[^>]*>\s*(\d+)`)},
	{"seats_available", regexp.MustCompile(`Enrollment Seats Available:</span>\s*<span[^>]*>\s*(\d+)`)},
	{"waitlist_total", regexp.MustCompile(`Waitlist Capacity:</span>\s*<span[^>]*>\s*(\d+)`)},
	{"waitlist_actual", regexp.MustCompile(`Waitlist Actual:</span>\s*<span[^>]*>\s*(\d+)`)},
	{"waitlist_available", regexp.MustCompile(`Waitlist Seats Available:</span>\s*<span[^>]*>\s*(\d+)`)},
}

// SeatStatus holds a CRN's live enrollment/waitlist counts; any field may
// be nil if the field wasn't present in the HTML fragment.
type SeatStatus struct {
	SeatsAvailable    *int
	SeatsTotal        *int
	WaitlistAvailable *int
	WaitlistTotal     *int
}

func matchInt(pattern *regexp.Regexp, html string) *int {
	m := pattern.FindStringSubmatch(html)
	if m == nil {
		return nil
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	return &v
}

// ParseSeatHTML scrapes the field regexes out of the live HTML fragment.
func ParseSeatHTML(html string) (SeatStatus, error) {
	values := make(map[string]*int, len(fieldPatterns))
	for _, fp := range fieldPatterns {
		values[fp.name] = matchInt(fp.pattern, html)
	}

	if values["seats_total"] == nil && values["waitlist_total"] == nil {
		return SeatStatus{}, fmt.Errorf("no enrollment data found for this CRN/term")
	}

	return SeatStatus{
		SeatsAvailable:    values["seats_available"],
		SeatsTotal:        values["seats_total"],
		WaitlistAvailable: values["waitlist_available"],
		WaitlistTotal:     values["waitlist_total"],
	}, nil
}

// FetchSeatStatus fetches and parses live status for one (term, crn).
func FetchSeatStatus(ctx context.Context, client *http.Client, term, crn string) (SeatStatus, error) {
	params := url.Values{"term": {term}, "crn": {crn}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LiveURL+"?"+params.Encode(), nil)
	if err != nil {
		return SeatStatus{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return SeatStatus{}, fmt.Errorf("failed to fetch live seats for CRN %s: %w", crn, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SeatStatus{}, fmt.Errorf("failed to fetch live seats for CRN %s: %w", crn, err)
	}
	if resp.StatusCode >= 400 {
		return SeatStatus{}, fmt.Errorf("failed to fetch live seats for CRN %s: HTTP %d", crn, resp.StatusCode)
	}
	return ParseSeatHTML(string(body))
}
