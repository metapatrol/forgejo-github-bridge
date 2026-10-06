package console

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// The list operations page, search and sort by mcp-gateway's rules
// (api/openapi-v1.yaml, PageLimit and PageCursor): limit is optional, a
// cursor names the item a page follows and is bound to the filters, q and
// sort it was handed out for.

const maxLimit = 1000

// errBadRequest marks an error the client made; it is answered with 400.
type errBadRequest struct{ msg string }

func (e errBadRequest) Error() string { return e.msg }

// token is what a cursor carries.
type token struct {
	// Filters are the list's filters, q included, and Sort its order, as
	// asked for when the cursor was handed out.
	Filters string         `json:"f"`
	Sort    string         `json:"s"`
	After   store.Position `json:"p"`
}

// paging is one request's paging: its limit and where it starts.
type paging struct {
	filters, sort string
	page          store.Page
}

// newPaging checks limit and cursor. filters names the request's filters,
// in a fixed order, so that a cursor can only be used with the same ones.
func newPaging(limit *int, cursor *string, sort string, filters ...string) (paging, error) {
	p := paging{filters: strings.Join(filters, "&"), sort: sort}
	p.page.Sort = sort
	if limit != nil {
		if *limit < 1 || *limit > maxLimit {
			return p, errBadRequest{fmt.Sprintf("limit must be between 1 and %d", maxLimit)}
		}
		p.page.Limit = *limit
	}
	if cursor != nil {
		raw, err := base64.RawURLEncoding.DecodeString(*cursor)
		var t token
		if err == nil {
			err = json.Unmarshal(raw, &t)
		}
		if err != nil {
			return p, errBadRequest{"cursor is not one this API handed out"}
		}
		if t.Filters != p.filters || t.Sort != p.sort {
			return p, errBadRequest{"cursor was handed out for other filters, q or sort"}
		}
		p.page.After = &t.After
	}
	return p, nil
}

// next encodes where the following page starts; nil on the last page.
func (p paging) next(after *store.Position) *string {
	if after == nil {
		return nil
	}
	raw, _ := json.Marshal(token{Filters: p.filters, Sort: p.sort, After: *after})
	c := base64.RawURLEncoding.EncodeToString(raw)
	return &c
}

// filter renders one filter for the fingerprint.
func filter[T any](name string, v *T) string {
	if v == nil {
		return name + "="
	}
	return fmt.Sprintf("%s=%v", name, *v)
}

// pageError answers a paging or sorting error: 400 when the client asked
// for something the list does not know, 500 otherwise.
func (s *server) pageError(w http.ResponseWriter, r *http.Request, err error) {
	var bad errBadRequest
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.msg)
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), store.ErrInvalid.Error()+": "))
	default:
		s.fail(w, r, err)
	}
}

// pageInMemory sorts and cuts a list that is not in the database — the
// configured repositories — by the same rules: value, then name; items
// without a value last whichever the direction.
func pageInMemory[T any](items []T, name func(T) string, columns map[string]func(T) store.SortValue,
	def string, p paging) ([]T, *store.Position, error) {
	sort := p.sort
	if sort == "" {
		sort = def
	}
	key, desc := strings.CutPrefix(sort, "-")
	column, ok := columns[key]
	if !ok {
		return nil, nil, errBadRequest{"cannot sort by " + sort}
	}
	type row struct {
		item  T
		value store.SortValue
		name  string
	}
	rows := make([]row, len(items))
	for i, it := range items {
		rows[i] = row{item: it, value: column(it), name: name(it)}
	}
	compare := func(av store.SortValue, an string, bv store.SortValue, bn string) int {
		switch {
		case av.IsNull() && bv.IsNull():
			return cmp.Compare(an, bn)
		case av.IsNull():
			return 1
		case bv.IsNull():
			return -1
		}
		c := compareValues(av, bv)
		if desc {
			c = -c
		}
		if c != 0 {
			return c
		}
		return cmp.Compare(an, bn)
	}
	slices.SortStableFunc(rows, func(a, b row) int { return compare(a.value, a.name, b.value, b.name) })
	start := 0
	if after := p.page.After; after != nil {
		if len(after.Tie) != 1 {
			return nil, nil, errBadRequest{"cursor is not one this API handed out"}
		}
		start = len(rows)
		for i, r := range rows {
			if compare(r.value, r.name, after.Value, after.Tie[0].Text) > 0 {
				start = i
				break
			}
		}
	}
	end := len(rows)
	var next *store.Position
	if p.page.Limit > 0 && start+p.page.Limit < end {
		end = start + p.page.Limit
		last := rows[end-1]
		next = &store.Position{Value: last.value, Tie: []store.SortValue{store.SortValueOf(last.name)}}
	}
	out := make([]T, 0, end-start)
	for _, r := range rows[start:end] {
		out = append(out, r.item)
	}
	return out, next, nil
}

func compareValues(a, b store.SortValue) int {
	switch {
	case a.Kind == "n" && b.Kind == "n":
		return cmp.Compare(a.Num, b.Num)
	case a.Kind == "d" && b.Kind == "d":
		return a.Time.Compare(b.Time)
	default:
		return cmp.Compare(a.Text, b.Text)
	}
}
