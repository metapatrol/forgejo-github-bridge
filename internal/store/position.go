package store

import (
	"fmt"
	"math"
	"time"
)

// Position is where a page of a list ends: the sort value of its last row
// and the values that break ties. It names a row, not an offset, so rows
// added or removed meanwhile neither repeat nor skip the ones after it.
type Position struct {
	Value SortValue   `json:"v"`
	Tie   []SortValue `json:"t"`
}

// SortValue is one value a row is sorted by: text, a number, a time, or
// nothing. It survives a round trip through JSON with its type, so that a
// cursor compares like with like.
type SortValue struct {
	Kind string    `json:"k,omitempty"` // "s", "n", "d"; empty is null
	Text string    `json:"s,omitempty"`
	Num  float64   `json:"n,omitempty"`
	Time time.Time `json:"d,omitzero"`
}

// SortValueOf wraps a value PostgreSQL returned.
func SortValueOf(v any) SortValue {
	switch v := v.(type) {
	case nil:
		return SortValue{}
	case string:
		return SortValue{Kind: "s", Text: v}
	case time.Time:
		return SortValue{Kind: "d", Time: v}
	case int64:
		return SortValue{Kind: "n", Num: float64(v)}
	case int32:
		return SortValue{Kind: "n", Num: float64(v)}
	case int:
		return SortValue{Kind: "n", Num: float64(v)}
	case float64:
		return SortValue{Kind: "n", Num: v}
	case bool:
		if v {
			return SortValue{Kind: "n", Num: 1}
		}
		return SortValue{Kind: "n", Num: 0}
	default:
		return SortValue{Kind: "s", Text: fmt.Sprint(v)}
	}
}

// IsNull reports whether the row had no value.
func (v SortValue) IsNull() bool { return v.Kind == "" }

// Param is the value as a query parameter: a whole number as an integer, so
// that it compares with an integer column.
func (v SortValue) Param() any {
	switch v.Kind {
	case "s":
		return v.Text
	case "d":
		return v.Time
	case "n":
		if v.Num == math.Trunc(v.Num) && math.Abs(v.Num) < 1<<53 {
			return int64(v.Num)
		}
		return v.Num
	}
	return nil
}
