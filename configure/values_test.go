package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func Test_LoadValuesContext(test *testing.T) {
	filename := "main.psy"
	literal := `
	value {
		tags = "foo=bar"
		tags_list = ["foo", "bar"]
	}`
	want := map[string]cty.Value{
		"tags":      cty.StringVal("foo=bar"),
		"tags_list": cty.TupleVal([]cty.Value{cty.StringVal("foo"), cty.StringVal("bar")}),
	}

	values, err := loadValuesContext(filename, []byte(literal))
	assert.Nil(test, err, "%s", err)
	assert.NotNil(test, values, "values is nil!")

	for k, v := range want {
		assert.Equal(test, v, values.Variables["value"].GetAttr(k))
	}
}
