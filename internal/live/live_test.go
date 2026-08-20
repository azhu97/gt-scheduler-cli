package live

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestParseSeatHTMLExtractsAllFields(t *testing.T) {
	html, err := os.ReadFile("testdata/live_sample.html")
	if err != nil {
		t.Fatal(err)
	}
	status, err := ParseSeatHTML(string(html))
	if err != nil {
		t.Fatal(err)
	}

	want := SeatStatus{
		SeatsAvailable:    intPtr(0),
		SeatsTotal:        intPtr(70),
		WaitlistAvailable: intPtr(25),
		WaitlistTotal:     intPtr(25),
	}
	if !seatStatusEqual(status, want) {
		t.Errorf("status = %+v, want %+v", dbg(status), dbg(want))
	}
}

func TestParseSeatHTMLRaisesOnUnrelatedContent(t *testing.T) {
	_, err := ParseSeatHTML("<html><body>not found</body></html>")
	if err == nil {
		t.Fatal("expected an error for HTML with no enrollment data")
	}
}

func TestFetchSeatStatusOverHTTP(t *testing.T) {
	fixture, err := os.ReadFile("testdata/live_sample.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("crn"); got != "12345" {
			t.Errorf("crn param = %q, want 12345", got)
		}
		w.Write(fixture)
	}))
	defer srv.Close()

	prev := LiveURL
	LiveURL = srv.URL
	defer func() { LiveURL = prev }()

	status, err := FetchSeatStatus(context.Background(), srv.Client(), "202302", "12345")
	if err != nil {
		t.Fatal(err)
	}
	if status.SeatsTotal == nil || *status.SeatsTotal != 70 {
		t.Errorf("seats_total = %v, want 70", status.SeatsTotal)
	}
}

func intPtr(v int) *int { return &v }

func seatStatusEqual(a, b SeatStatus) bool {
	return intPtrEqual(a.SeatsAvailable, b.SeatsAvailable) &&
		intPtrEqual(a.SeatsTotal, b.SeatsTotal) &&
		intPtrEqual(a.WaitlistAvailable, b.WaitlistAvailable) &&
		intPtrEqual(a.WaitlistTotal, b.WaitlistTotal)
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func dbg(s SeatStatus) string {
	fmtP := func(v *int) string {
		if v == nil {
			return "nil"
		}
		return fmt.Sprintf("%d", *v)
	}
	return fmtP(s.SeatsAvailable) + "/" + fmtP(s.SeatsTotal) + "/" + fmtP(s.WaitlistAvailable) + "/" + fmtP(s.WaitlistTotal)
}
