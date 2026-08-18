from pathlib import Path

import pytest

from gtclass.live import LiveDataError, SeatStatus, parse_seat_html

FIXTURE = Path(__file__).parent / "fixtures" / "live_sample.html"


def test_parse_seat_html_extracts_all_fields():
    html = FIXTURE.read_text()
    status = parse_seat_html(html)
    assert status == SeatStatus(
        seats_available=0,
        seats_total=70,
        waitlist_available=25,
        waitlist_total=25,
    )


def test_parse_seat_html_raises_on_unrelated_content():
    with pytest.raises(LiveDataError):
        parse_seat_html("<html><body>not found</body></html>")
