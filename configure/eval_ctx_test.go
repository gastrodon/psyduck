package configure

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func Test_makeEvalCtx(test *testing.T) {
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

	values, err := makeEvalCtx(filename, []byte(literal))
	assert.Nil(test, err, "%s", err)
	assert.NotNil(test, values, "values is nil!")

	// panic(fmt.Sprintf("%+v", values.Variables["value"]))

	for k, v := range want {
		assert.Equal(test, v, values.Variables["value"].GetAttr(k))
	}
}
func Test_makeEvalCtx_Number(test *testing.T) {
	filename := "main.psy"
	literal := `
	value {
		v = 1234
	}`

	values, err := makeEvalCtx(filename, []byte(literal))
	assert.Nil(test, err, "%s", err)
	assert.NotNil(test, values, "values is nil!")

	i, _ := values.Variables["value"].GetAttr("v").AsBigFloat().Int64()
	assert.Equal(test, int64(1234), i)
}

func Test_makeEvalCtx_Env(test *testing.T) {
	filename := "main.psy"
	literal := ``

	os.Setenv("FOO", "bar")
	defer os.Unsetenv("FOO")
	values, err := makeEvalCtx(filename, []byte(literal))
	assert.Nil(test, err, "%s", err)
	assert.NotNil(test, values, "values is nil!")

	assert.Equal(test, cty.StringVal("bar"), values.Variables["env"].GetAttr("FOO"))
}
