package configure

import (
	"math/big"
	"os"
	"testing"

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

	if values == nil {
		test.Error("values is nil!")
	}

	// panic(fmt.Sprintf("%+v", values.Variables["value"]))

	for k, v := range want {
		if !v.Equals(values.Variables["value"].GetAttr(k)).True() {
			test.Errorf("for %s, expected %v, got %v", k, v, values.Variables["value"].GetAttr(k))
		}
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

	if values == nil {
		test.Error("values is nil!")
	}
	expected := cty.NumberVal(new(big.Float).SetInt64(1234).SetPrec(512))
	actual := values.Variables["value"].GetAttr("v")
	if !expected.Equals(actual).True() {
		test.Errorf("expected %v, got %v", expected, actual)
	}
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

	if values == nil {
		test.Error("values is nil!")
	}
	expected := cty.StringVal("bar")
	actual := values.Variables["env"].GetAttr("FOO")
	if !expected.Equals(actual).True() {
		test.Errorf("expected %v, got %v", expected, actual)
	}
}
