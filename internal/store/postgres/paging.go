package postgres

import (
	"fmt"
	"strconv"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// The console's lists page by key, not by offset, and sort by the rules
// mcp-gateway's inspect API follows: a column, descending with a leading
// "-"; items without a value last whichever the direction; ties broken by a
// key that never changes, so the order is total. A page is "the rows after
// this one", which in SQL is a keyset condition on the sort expression and
// the tiebreak.

// keyset is one sort order of one list.
type keyset struct {
	// expr is the SQL expression sorted by.
	expr string
	desc bool
	// tie are the expressions that break ties, ascending; together they are
	// unique per row.
	tie []string
}

// sortKeys looks a sort parameter up among a list's columns.
func sortKeys(sort, def string, columns map[string]string, tie []string) (keyset, error) {
	if sort == "" {
		sort = def
	}
	name, desc := strings.CutPrefix(sort, "-")
	expr, ok := columns[name]
	if !ok {
		return keyset{}, fmt.Errorf("%w: cannot sort by %q", store.ErrInvalid, sort)
	}
	return keyset{expr: expr, desc: desc, tie: tie}, nil
}

// orderBy is the ORDER BY clause.
func (k keyset) orderBy() string {
	dir := "ASC"
	if k.desc {
		dir = "DESC"
	}
	return " ORDER BY (" + k.expr + ") IS NULL, " + k.expr + " " + dir + ", " + strings.Join(k.tie, ", ")
}

// selectKeys are the extra columns a row's position is read from: the sort
// value, then the tiebreak.
func (k keyset) selectKeys() string {
	return k.expr + ", " + strings.Join(k.tie, ", ")
}

// after is the condition for the rows that follow p. arg adds a parameter
// and returns its placeholder.
func (k keyset) after(p *store.Position, arg func(any) string) (string, error) {
	if len(p.Tie) != len(k.tie) {
		return "", fmt.Errorf("%w: cursor is not one this API handed out", store.ErrInvalid)
	}
	tieValues := make([]string, len(p.Tie))
	for i, v := range p.Tie {
		tieValues[i] = arg(v.Param())
	}
	ties := "(" + strings.Join(k.tie, ", ") + ") > (" + strings.Join(tieValues, ", ") + ")"
	if p.Value.IsNull() {
		// Rows without a value come last: only those, after the tie.
		return "((" + k.expr + ") IS NULL AND " + ties + ")", nil
	}
	op := ">"
	if k.desc {
		op = "<"
	}
	v := arg(p.Value.Param())
	return "((" + k.expr + ") IS NULL OR " + k.expr + " " + op + " " + v +
		" OR (" + k.expr + " = " + v + " AND " + ties + "))", nil
}

// position reads a row's position from the values of selectKeys.
func position(values []any) store.Position {
	p := store.Position{Value: store.SortValueOf(values[0])}
	for _, v := range values[1:] {
		p.Tie = append(p.Tie, store.SortValueOf(v))
	}
	return p
}

// placeholders makes the arg helper the list queries share.
func placeholders(args *[]any) func(any) string {
	return func(v any) string {
		*args = append(*args, v)
		return "$" + strconv.Itoa(len(*args))
	}
}
