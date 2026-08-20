package poller

import (
	"strings"
	"testing"

	"github.com/azhu97/gt-scheduler-cli/internal/live"
)

func p(v int) *int { return &v }

func TestDiffNoPreviousSnapshotIsSilent(t *testing.T) {
	curr := live.SeatStatus{SeatsAvailable: p(0), SeatsTotal: p(70), WaitlistAvailable: p(25), WaitlistTotal: p(25)}
	if got := diff(nil, curr); len(got) != 0 {
		t.Errorf("diff(nil, ...) = %v, want empty", got)
	}
}

func TestDiffFlagsSeatsOpeningUp(t *testing.T) {
	prev := &snapshotRow{SeatsAvailable: p(0), WaitlistAvailable: p(25)}
	curr := live.SeatStatus{SeatsAvailable: p(3), SeatsTotal: p(70), WaitlistAvailable: p(25), WaitlistTotal: p(25)}
	changes := diff(prev, curr)
	found := false
	for _, c := range changes {
		if strings.Contains(c, "OPENED UP") {
			found = true
		}
	}
	if !found {
		t.Errorf("diff = %v, want one entry containing OPENED UP", changes)
	}
}

func TestDiffFlagsWaitlistMovement(t *testing.T) {
	prev := &snapshotRow{SeatsAvailable: p(0), WaitlistAvailable: p(25)}
	curr := live.SeatStatus{SeatsAvailable: p(0), SeatsTotal: p(70), WaitlistAvailable: p(20), WaitlistTotal: p(25)}
	changes := diff(prev, curr)
	found := false
	for _, c := range changes {
		if strings.Contains(c, "waitlist available 25 -> 20") {
			found = true
		}
	}
	if !found {
		t.Errorf("diff = %v, want one entry with 'waitlist available 25 -> 20'", changes)
	}
}

func TestDiffNoChangesWhenIdentical(t *testing.T) {
	prev := &snapshotRow{SeatsAvailable: p(5), WaitlistAvailable: p(0)}
	curr := live.SeatStatus{SeatsAvailable: p(5), SeatsTotal: p(70), WaitlistAvailable: p(0), WaitlistTotal: p(25)}
	if got := diff(prev, curr); len(got) != 0 {
		t.Errorf("diff = %v, want empty", got)
	}
}
