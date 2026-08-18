import json
from pathlib import Path

from gtclass.gtdata import parse_term_catalog, parse_term_prereqs

FIXTURE = Path(__file__).parent / "fixtures" / "sample_term.json"


def load_fixture() -> dict:
    return json.loads(FIXTURE.read_text())


def test_parse_term_catalog_basic_section():
    rows = parse_term_catalog(load_fixture(), term="202302")
    by_crn = {r["crn"]: r for r in rows}

    row = by_crn["25096"]
    assert row["subject"] == "ACCT"
    assert row["course_number"] == "2101"
    assert row["section"] == "A"
    assert row["title"] == "Accounting I"
    assert row["meeting_days"] == "MW"
    assert row["meeting_time"] == "12:30 pm - 1:45 pm"
    assert row["location"] == "Scheller College of Business 103"
    assert row["instructor"] == "Eric R Condie (P)"
    assert row["credits"] == 3.0


def test_parse_term_catalog_handles_tba_section():
    rows = parse_term_catalog(load_fixture(), term="202302")
    by_crn = {r["crn"]: r for r in rows}

    # QH section has days "&nbsp;" and room "TBA" upstream.
    tba_row = by_crn["34617"]
    assert tba_row["meeting_days"] == "TBA"
    assert tba_row["location"] == "TBA"

    row = by_crn["25594"]
    assert row["subject"] == "CS"
    assert row["course_number"] == "1331"
    assert row["credits"] == 3.0


def test_parse_term_catalog_row_count():
    rows = parse_term_catalog(load_fixture(), term="202302")
    # 4 ACCT 2101 sections + 7 CS 1331 sections
    assert len(rows) == 11
    assert all(r["term"] == "202302" for r in rows)


def test_parse_term_prereqs_no_prereqs():
    rows = parse_term_prereqs(load_fixture(), term="202302")
    by_key = {(r["subject"], r["course_number"]): r for r in rows}

    row = by_key[("ACCT", "2101")]
    assert row["title"] == "Accounting I"
    assert json.loads(row["prereqs_json"]) == []


def test_parse_term_prereqs_nested_expression():
    rows = parse_term_prereqs(load_fixture(), term="202302")
    by_key = {(r["subject"], r["course_number"]): r for r in rows}

    row = by_key[("CS", "1331")]
    tree = json.loads(row["prereqs_json"])
    assert tree[0] == "or"
    assert {"id": "CS 1301", "grade": "C"} in tree
    assert {"id": "CS 1315", "grade": "C"} in tree
