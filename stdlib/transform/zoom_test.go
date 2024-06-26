package transform

import (
	"testing"

	"github.com/psyduck-etl/sdk"
)

var cases = []struct {
	Field  string
	Source []byte
	Want   []byte
}{
	{
		Field:  "who",
		Source: []byte(`{"who": {"cat": "huge"}}`),
		Want:   []byte(`{"cat": "huge"}`),
	},
	{
		Field:  "cats",
		Source: []byte(`{"cats": ["huge", "alice", "edward", "pixie"]}`),
		Want:   []byte(`["huge", "alice", "edward", "pixie"]`),
	},
	{
		Field:  "phoenix",
		Source: []byte(`{"alice": "black", "edward": "black", "huge": "blackwhite", "phoenix": "orange", "pixie": "tortie"}`),
		Want:   []byte(`orange`),
	},
}

func TestZoom(test *testing.T) {
	for index, testcase := range cases {
		parse := func(target interface{}) error {
			target.(*zoomConfig).Field = testcase.Field

			return nil
		}

		transformer, err := Zoom(parse)
		if err != nil {
			test.Fatal(err)
		}

		zoomed, err := transformer(testcase.Source)
		if err != nil {
			test.Fatal(err)
		}

		if !sdk.SameBytes(zoomed, testcase.Want) {
			test.Fatalf("zoomed does not match #%d! \nzoomed: %s %v\nwant: %s %v",
				index, zoomed, zoomed, testcase.Want, testcase.Want)
		}
	}
}
