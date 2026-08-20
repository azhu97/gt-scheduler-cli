package formatting

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
)

// MaxPrereqDepth guards against pathological/cyclic prereq trees when
// recursing into referenced courses in PrintPrereqTree.
const MaxPrereqDepth = 6

type treeNode struct {
	label    string
	children []*treeNode
}

func (n *treeNode) add(label string) *treeNode {
	child := &treeNode{label: label}
	n.children = append(n.children, child)
	return child
}

func (n *treeNode) addChild(child *treeNode) {
	n.children = append(n.children, child)
}

func renderTree(w io.Writer, node *treeNode) {
	fmt.Fprintln(w, node.label)
	renderChildren(w, node.children, "")
}

func renderChildren(w io.Writer, children []*treeNode, prefix string) {
	for i, c := range children {
		last := i == len(children)-1
		connector, nextPrefix := "├── ", prefix+"│   "
		if last {
			connector, nextPrefix = "└── ", prefix+"    "
		}
		fmt.Fprintln(w, prefix+connector+c.label)
		renderChildren(w, c.children, nextPrefix)
	}
}

func splitCourseID(id string) (subject, courseNumber string) {
	subject, courseNumber, _ = strings.Cut(id, " ")
	return subject, courseNumber
}

func courseLabel(subject, courseNumber, title, grade string) string {
	label := color.New(color.Bold).Sprintf("%s %s", subject, courseNumber)
	if title != "" {
		label += " — " + title
	}
	if grade != "" {
		label += " " + color.New(color.Faint).Sprintf("(min grade %s)", grade)
	}
	return label
}

func dim(s string) string {
	return color.New(color.Faint).Sprint(s)
}

type prereqRow struct {
	title       string
	prereqsJSON string
	found       bool
}

func lookupPrereqRow(db *sql.DB, term, subject, courseNumber string) (prereqRow, error) {
	var row prereqRow
	err := db.QueryRow(
		"SELECT title, prereqs_json FROM course_prereqs WHERE term = ? AND subject = ? AND course_number = ?",
		term, subject, courseNumber,
	).Scan(&row.title, &row.prereqsJSON)
	if err == sql.ErrNoRows {
		return prereqRow{}, nil
	}
	if err != nil {
		return prereqRow{}, err
	}
	row.found = true
	return row, nil
}

// leafCourseLabel is the label for a course reference with no further
// recursion (used when direct_only/--min is set).
func leafCourseLabel(db *sql.DB, term, subject, courseNumber, grade string) (string, error) {
	row, err := lookupPrereqRow(db, term, subject, courseNumber)
	if err != nil {
		return "", err
	}
	return courseLabel(subject, courseNumber, row.title, grade), nil
}

type courseKey [2]string

func cloneAncestors(ancestors map[courseKey]bool, add courseKey) map[courseKey]bool {
	next := make(map[courseKey]bool, len(ancestors)+1)
	for k := range ancestors {
		next[k] = true
	}
	next[add] = true
	return next
}

// prereqCourseNode builds the tree node for one course, recursing into its
// own prerequisites.
func prereqCourseNode(db *sql.DB, term, subject, courseNumber, grade string, ancestors map[courseKey]bool, depth int, directOnly bool) (*treeNode, error) {
	row, err := lookupPrereqRow(db, term, subject, courseNumber)
	if err != nil {
		return nil, err
	}

	key := courseKey{subject, courseNumber}
	node := &treeNode{label: courseLabel(subject, courseNumber, row.title, grade)}

	if !row.found {
		node.add(dim("(not offered this term — no prerequisite data)"))
		return node, nil
	}
	if ancestors[key] {
		node.add(dim("(already shown above — skipping to avoid a cycle)"))
		return node, nil
	}
	if depth >= MaxPrereqDepth {
		node.add(dim("(max depth reached)"))
		return node, nil
	}

	var prereqs any
	if row.prereqsJSON != "" {
		if err := json.Unmarshal([]byte(row.prereqsJSON), &prereqs); err != nil {
			return nil, err
		}
	}
	if isEmptyExpr(prereqs) {
		node.add(dim("(no prerequisites)"))
	} else if err := addPrereqExpr(node, db, term, prereqs, cloneAncestors(ancestors, key), depth+1, directOnly); err != nil {
		return nil, err
	}
	return node, nil
}

func isEmptyExpr(expr any) bool {
	if expr == nil {
		return true
	}
	if arr, ok := expr.([]any); ok {
		return len(arr) == 0
	}
	return false
}

func addPrereqExpr(node *treeNode, db *sql.DB, term string, expr any, ancestors map[courseKey]bool, depth int, directOnly bool) error {
	if isEmptyExpr(expr) {
		return nil
	}

	switch v := expr.(type) {
	case map[string]any:
		id, _ := v["id"].(string)
		subject, courseNumber := splitCourseID(id)
		if subject == "" || courseNumber == "" {
			return nil
		}
		grade, _ := v["grade"].(string)
		if directOnly {
			label, err := leafCourseLabel(db, term, subject, courseNumber, grade)
			if err != nil {
				return err
			}
			node.add(label)
			return nil
		}
		child, err := prereqCourseNode(db, term, subject, courseNumber, grade, ancestors, depth, directOnly)
		if err != nil {
			return err
		}
		node.addChild(child)
		return nil

	case []any:
		if len(v) == 0 {
			return nil
		}
		op, ok := v[0].(string)
		if !ok || (op != "and" && op != "or") {
			return nil
		}
		opNode := node.add(color.New(color.Italic).Sprint(strings.ToUpper(op)))
		for _, child := range v[1:] {
			if err := addPrereqExpr(opNode, db, term, child, ancestors, depth, directOnly); err != nil {
				return err
			}
		}
	}
	return nil
}

// PrintPrereqTree renders the prerequisite tree for one course.
func PrintPrereqTree(w io.Writer, db *sql.DB, term, subject, courseNumber string, directOnly bool) error {
	root, err := prereqCourseNode(db, term, subject, courseNumber, "", map[courseKey]bool{}, 0, directOnly)
	if err != nil {
		return err
	}
	renderTree(w, root)
	return nil
}
