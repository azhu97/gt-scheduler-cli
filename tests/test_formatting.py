import json
import sqlite3

from rich.console import Console

from gtclass import db
from gtclass.formatting import print_prereq_tree


def make_conn() -> sqlite3.Connection:
    conn = sqlite3.connect(":memory:")
    conn.row_factory = sqlite3.Row
    db.init_db(conn)
    return conn


def insert_prereq(conn: sqlite3.Connection, term: str, subject: str, number: str, title: str, prereqs) -> None:
    conn.execute(
        """
        INSERT INTO course_prereqs (term, subject, course_number, title, prereqs_json)
        VALUES (?, ?, ?, ?, ?)
        """,
        (term, subject, number, title, json.dumps(prereqs)),
    )
    conn.commit()


def render(conn: sqlite3.Connection, term: str, subject: str, number: str, direct_only: bool = False) -> str:
    console = Console(record=True, width=100)
    import gtclass.formatting as formatting

    original = formatting.console
    formatting.console = console
    try:
        print_prereq_tree(conn, term, subject, number, direct_only=direct_only)
    finally:
        formatting.console = original
    return console.export_text()


def test_print_prereq_tree_no_prereqs():
    conn = make_conn()
    insert_prereq(conn, "202302", "CS", "1301", "Intro Computing", [])

    out = render(conn, "202302", "CS", "1301")
    assert "CS 1301" in out
    assert "no prerequisites" in out


def test_print_prereq_tree_recurses_into_nested_course():
    conn = make_conn()
    insert_prereq(conn, "202302", "CS", "1332", "Data Structures", ["or", {"id": "CS 1331", "grade": "C"}])
    insert_prereq(conn, "202302", "CS", "1331", "Intro OOP", [])

    out = render(conn, "202302", "CS", "1332")
    assert "CS 1332" in out
    assert "CS 1331" in out
    assert "min grade C" in out


def test_print_prereq_tree_handles_unresolvable_reference():
    conn = make_conn()
    insert_prereq(conn, "202302", "CS", "3510", "Algorithms", ["or", {"id": "MATH 3012", "grade": "D"}])

    out = render(conn, "202302", "CS", "3510")
    assert "MATH 3012" in out
    assert "no prerequisite data" in out


def test_print_prereq_tree_direct_only_skips_grandchildren():
    conn = make_conn()
    insert_prereq(conn, "202302", "CS", "1332", "Data Structures", ["or", {"id": "CS 1331", "grade": "C"}])
    insert_prereq(conn, "202302", "CS", "1331", "Intro OOP", ["or", {"id": "CS 1301", "grade": "C"}])
    insert_prereq(conn, "202302", "CS", "1301", "Intro Computing", [])

    out = render(conn, "202302", "CS", "1332", direct_only=True)
    assert "CS 1332" in out
    assert "CS 1331" in out
    # Direct-only mode must not expand CS 1331's own prerequisites.
    assert "CS 1301" not in out
    assert "no prerequisites" not in out


def test_print_prereq_tree_avoids_infinite_cycle():
    conn = make_conn()
    # Contrived self-referential cycle: A depends on B, B depends on A.
    insert_prereq(conn, "202302", "CS", "A", "Course A", ["or", {"id": "CS B"}])
    insert_prereq(conn, "202302", "CS", "B", "Course B", ["or", {"id": "CS A"}])

    out = render(conn, "202302", "CS", "A")
    assert "already shown above" in out
