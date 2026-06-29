package configure

import (
	"math/big"
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

	values, diags := makeEvalCtx(filename, []byte(literal))
	if diags.HasErrors() {
		test.Fatalf("make-eval-ctx: %s", drawDiags(diags))
	}

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

	values, diags := makeEvalCtx(filename, []byte(literal))
	if diags.HasErrors() {
		test.Fatalf("make-eval-ctx has numbers: %s", drawDiags(diags))
	}

	assert.NotNil(test, values, "values is nil!")
	assert.Equal(test, cty.NumberVal(new(big.Float).SetInt64(1234).SetPrec(512)), values.Variables["value"].GetAttr("v"))
}

func Test_makeEvalCtx_Env(test *testing.T) {
	filename := "main.psy"
	literal := ``

	os.Setenv("FOO", "bar")
	defer os.Unsetenv("FOO")
	values, diags := makeEvalCtx(filename, []byte(literal))
	if diags.HasErrors() {
		test.Fatalf("make-eval-ctx has env: %s", drawDiags(diags))
	}

	assert.NotNil(test, values, "values is nil!")
	assert.Equal(test, cty.StringVal("bar"), values.Variables["env"].GetAttr("FOO"))
}
