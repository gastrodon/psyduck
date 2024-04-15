package transform

import (
	"testing"

	"github.com/psyduck-etl/sdk"
)

func TestSprintf(t *testing.T) {
	testcases := [...]struct {
		encoding, format string
		in, want         []byte
	}{
		{"bytes", "foo %d", []byte{32}, []byte("foo 32")},
		{"string", "foo %s", []byte("bar"), []byte("foo bar")},
	}

	for _, tc := range testcases {
		transform, err := Sprintf(func(i interface{}) error {
			i.(*sprintfConfig).Format = tc.format
			i.(*sprintfConfig).Encoding = tc.encoding
			return nil
		})

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
