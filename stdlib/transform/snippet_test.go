package transform

import (
	"testing"

	"github.com/psyduck-etl/sdk"
)

func TestSnippet(t *testing.T) {
	testcases := [...]struct {
		fields   []string
		in, want []byte
	}{{[]string{"huge"}, []byte(`{"phoenix":"quiet", "huge":"very loud"}`), []byte(`{"huge":"very loud"}`)}}

	for _, tc := range testcases {
		transform, err := Snippet(func(i interface{}) error {
			i.(*snippetConfig).Fields = tc.fields
			return nil
		}, nil)

		if err != nil {
			t.Fatalf("failed to form transformer: %s", err)
		}

		out, err := transform(tc.in)
		if err != nil {
			t.Fatalf("failed to transform: %s", err)
		}

		if !sdk.SameBytes(out, tc.want) {
			t.Fatalf("out doesn't match want: %s %v != %s %v", out, out, tc.want, tc.want)
		}
	}
}
