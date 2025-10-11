package transform

import (
	"encoding/json"
	"testing"

	"github.com/psyduck-etl/sdk"
)

func Test_readField(t *testing.T) {
	testcases := [...]struct {
		have, want []byte
		field      []string
	}{
		{[]byte(`{"foo": "bar"}`), []byte(`bar`), []string{"foo"}},
		{[]byte(`{"foo": {"bar": "baz"}}`), []byte(`{"bar": "baz"}`), []string{"foo"}},
		{[]byte(`{"foo": {"bar": "baz"}}`), []byte(`baz`), []string{"foo", "bar"}},
	}

	for i, tc := range testcases {
		t.Logf("testcase %d: %s -> %s\n", i, string(tc.have), string(tc.want))
		b := make(map[string]zoomTarget)
		if err := json.Unmarshal(tc.have, &b); err != nil {
			t.Fatalf("read-field[%d]: failed unmarshaling: %s, err!", i, err)
		}

		d, err := readField(b, tc.field)
		if err != nil {
			t.Fatalf("read-field[%d]: failed reading field: %s, err!", i, err)
		}
		if !sdk.SameBytes(d, tc.want) {
			t.Fatalf("read-field[%d]: failed reading field: expected %v, got %v!", i, tc.want, d)
		}
	}
}

func Test_Transpose(t *testing.T) {
	testcases := [...]struct {
		have   []byte
		want   map[string]string
		fields map[string][]string
	}{{
		[]byte(`{"cats": {
			"huge": {"color": "blackwhite", "size": "skinty"},
			"alice": {"size": "large", "color": "black"},
			"pixie": {"size": "medium"}
		}}`),
		map[string]string{
			"huge_size":  "skinty",
			"alice_size": "large",
			"pixie_size": "medium",
		},
		map[string][]string{
			"huge_size":  {"cats", "huge", "size"},
			"alice_size": {"cats", "alice", "size"},
			"pixie_size": {"cats", "pixie", "size"},
		},
	}}

	for i, tc := range testcases {
		t.Logf("testcase %d: %s -> %v\n", i, string(tc.have), tc.want)
		transform, err := Transpose(func(target interface{}) error {
			target.(*transposeConfig).Fields = tc.fields
			return nil
		})

		if err != nil {
			t.Fatalf("transpose[%d]: failed creating transpose: %s, err!", i, err)
		}

		posed, err := transform(tc.have)
		if err != nil {
			t.Fatalf("transpose[%d]: failed transforming: %s, err!", i, err)
		}

		posedMap := make(map[string]string)
		if err := json.Unmarshal(posed, &posedMap); err != nil {
			t.Fatalf("transpose[%d]: failed unmarshaling: %s, err!", i, err)
		}

		if len(tc.want) != len(posedMap) {
			t.Fatalf("transpose[%d]: failed transposing: expected %d keys, got %d!", i, len(tc.want), len(posedMap))
		}
		for k, v := range tc.want {
			if got, ok := posedMap[k]; !ok || got != v {
				t.Fatalf("transpose[%d]: failed transposing key %s: expected %s, got %s!", i, k, v, got)
			}
		}
	}
}
