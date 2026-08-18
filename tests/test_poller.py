from gtclass.live import SeatStatus
from gtclass.poller import _diff


def test_diff_no_previous_snapshot_is_silent():
    curr = SeatStatus(seats_available=0, seats_total=70, waitlist_available=25, waitlist_total=25)
    assert _diff(None, curr) == []


def test_diff_flags_seats_opening_up():
    prev = {"seats_available": 0, "waitlist_available": 25}
    curr = SeatStatus(seats_available=3, seats_total=70, waitlist_available=25, waitlist_total=25)
    changes = _diff(prev, curr)
    assert any("OPENED UP" in c for c in changes)


def test_diff_flags_waitlist_movement():
    prev = {"seats_available": 0, "waitlist_available": 25}
    curr = SeatStatus(seats_available=0, seats_total=70, waitlist_available=20, waitlist_total=25)
    changes = _diff(prev, curr)
    assert any("waitlist available 25 -> 20" in c for c in changes)


def test_diff_no_changes_when_identical():
    prev = {"seats_available": 5, "waitlist_available": 0}
    curr = SeatStatus(seats_available=5, seats_total=70, waitlist_available=0, waitlist_total=25)
    assert _diff(prev, curr) == []
