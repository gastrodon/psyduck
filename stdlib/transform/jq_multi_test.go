package transform

import (
	"reflect"
	"testing"
)

// Jq is explosive: a jq expression yields a stream of 0, 1, or many values per
// input, and each value must become its own output message. Regression for the
// bug where Jq emitted only the first value of the iterator and silently
// dropped the rest.
func TestJq_Explodes(t *testing.T) {
	cases := []struct {
		name, expr, in string
		want           []string
	}{
		{"iterate array", ".[]", `[1,2,3]`, []string{"1", "2", "3"}},
		{"comma outputs", ".a, .b", `{"a":10,"b":20}`, []string{"10", "20"}},
		{"generator", "range(3)", `null`, []string{"0", "1", "2"}},
		{"single output", ".a", `{"a":7}`, []string{"7"}},
		{"empty drops", "empty", `{"a":1}`, nil},
		{"nested iterate", ".items[]", `{"items":["x","y"]}`, []string{"x", "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := build(t, Jq, map[string]any{"expression": tc.expr})
			got := runAll(t, fn, tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("jq %q over %s: want %v, got %v", tc.expr, tc.in, tc.want, got)
			}
		})
	}
}

// A per-message error mid-stream is still forwarded on errs and the stream
// keeps running — explosion must not break that. The first input fails to
// parse; the second explodes into two outputs.
func TestJq_ErrorThenExplodeContinues(t *testing.T) {
	fn := build(t, Jq, map[string]any{"expression": ".[]"})
	got, errs := runAllErrs(t, fn, `notjson`, `[1,2]`)
	if !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("want [1 2] after the bad message, got %v", got)
	}
	if len(errs) != 1 {
		t.Fatalf("want 1 forwarded error, got %v", errs)
	}
}
