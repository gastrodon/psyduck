package hcl

import (
	"encoding/json"
	"testing"

	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
)

// blockWith builds an hclBlock directly from cty values, skipping the
// parser. Encode only ever sees already-evaluated values (evalValues runs
// eagerly at parse time), so this is the exact input Encode gets in prod.
func blockWith(values map[string]cty.Value) *hclBlock {
	return &hclBlock{values: values, origin: sdk.SourceRange{SourceName: "test"}}
}

func TestBlockEncode_Empty(t *testing.T) {
	raw, err := blockWith(map[string]cty.Value{}).Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(raw) != "{}" {
		t.Errorf("Encode() = %q, want %q", raw, "{}")
	}
}

func TestBlockEncode_Null(t *testing.T) {
	// A non-required attribute with no default lands in values as a typed
	// null (see evalValues). Encode must keep it — the far side reads null
	// as "absent" and leaves its struct field at the zero value.
	raw, err := blockWith(map[string]cty.Value{
		"value": cty.NullVal(cty.String),
	}).Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Encode produced invalid JSON %q: %v", raw, err)
	}
	if v, ok := got["value"]; !ok || v != nil {
		t.Errorf("Encode() = %q, want a JSON null for \"value\"", raw)
	}
}

// TestBlockEncode_RoundTrip is the real wire contract: an hclBlock's
// Encode output must decode, through the SDK's own JSON block, into the
// same values a plugin subprocess would see. Covers each scalar kind plus
// a list and a null.
func TestBlockEncode_RoundTrip(t *testing.T) {
	block := blockWith(map[string]cty.Value{
		"name":    cty.StringVal("hello"),
		"count":   cty.NumberIntVal(42),
		"ratio":   cty.NumberFloatVal(1.5),
		"enabled": cty.True,
		"tags":    cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
		"absent":  cty.NullVal(cty.String),
	})

	raw, err := block.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	type config struct {
		Name    string   `psy:"name"`
		Count   int      `psy:"count"`
		Ratio   float64  `psy:"ratio"`
		Enabled bool     `psy:"enabled"`
		Tags    []string `psy:"tags"`
		Absent  string   `psy:"absent"`
	}
	got := &config{}
	if err := sdk.NewJSONBlock(block.origin, raw).Decode(got); err != nil {
		t.Fatalf("Decode across the wire: %v", err)
	}

	want := &config{
		Name:    "hello",
		Count:   42,
		Ratio:   1.5,
		Enabled: true,
		Tags:    []string{"a", "b"},
		Absent:  "", // null → untouched
	}
	if got.Name != want.Name || got.Count != want.Count || got.Ratio != want.Ratio ||
		got.Enabled != want.Enabled || got.Absent != want.Absent {
		t.Errorf("round trip scalars = %+v, want %+v", got, want)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("round trip Tags = %v, want [a b]", got.Tags)
	}
}

// TestBlockEncode_Map covers the map decoder path (distinct from list):
// a cty map must cross the wire as a JSON object and rebuild into a Go map.
func TestBlockEncode_Map(t *testing.T) {
	block := blockWith(map[string]cty.Value{
		"headers": cty.MapVal(map[string]cty.Value{"a": cty.StringVal("1"), "b": cty.StringVal("2")}),
		"empty":   cty.ListValEmpty(cty.String),
	})
	raw, err := block.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got := &struct {
		Headers map[string]string `psy:"headers"`
		Empty   []string          `psy:"empty"`
	}{}
	if err := sdk.NewJSONBlock(block.origin, raw).Decode(got); err != nil {
		t.Fatalf("Decode across the wire: %v", err)
	}
	if got.Headers["a"] != "1" || got.Headers["b"] != "2" || len(got.Headers) != 2 {
		t.Errorf("round trip Headers = %v, want map[a:1 b:2]", got.Headers)
	}
	if got.Empty == nil || len(got.Empty) != 0 {
		t.Errorf("round trip Empty = %v, want non-nil empty slice", got.Empty)
	}
}
