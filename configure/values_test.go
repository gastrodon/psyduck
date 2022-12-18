package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func testableValues(entries map[string]cty.Value) *Values {
	return &Values{
		Blocks: []struct {
			Entries map[string]cty.Value `hcl:",remain"`
		}{{Entries: entries}},
	}
}

func TestLoadValues(test *testing.T) {
	cases := []struct {
		Literal  string
		Filename string
		Want     *Values
	}{
		{
			`value {
				tags = "foo=bar"
			}`,
			"main.psy",
			testableValues(map[string]cty.Value{
				"tags": cty.StringVal("foo=bar"),
			}),
		},
	}

	for _, testcase := range cases {
		values, err := loadValues(testcase.Filename, []byte(testcase.Literal))
		assert.Nil(test, err, "%s", err)
		assert.NotNil(test, values, "values is nil!")
		assert.Equal(test, values, testcase.Want)
	}
}
