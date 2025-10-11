package configure

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
)

func drawDiags(d hcl.Diagnostics) string {
	buf := make([]string, len(d))
	for i, diag := range d {
		buf[i] = diag.Error()
	}

	return strings.Join(buf, "\n")
}

func TestParsePlugins(test *testing.T) {
	cases := []struct {
		Literal string
		Want    []PluginDesc
	}{
		{
			`plugin "psyduck"  {
				source = "/std.so"
			}`,
			[]PluginDesc{
				{Name: "psyduck", Source: "/std.so"},
			},
		},
		{
			`plugin "psyduck" {
				source = "/std.so"
			}

			produce "foo" "bar" {
				foo = "bar"
			}`,
			[]PluginDesc{
				{Name: "psyduck", Source: "/std.so"},
			},
		},
	}

	for i, testcase := range cases {
		plugins, diags := ParsePluginsDesc("parse-plugin.psy", []byte(testcase.Literal))
		if diags.HasErrors() {
			test.Errorf("unexpected errors: %s", diags)
		}
		if diags.HasErrors() {
			test.Fatalf("parse-plugin[%d] has errs: %s", i, drawDiags(diags))
		}

		if plugins == nil {
			test.Error("plugins is nil!")
		}
		if len(testcase.Want) == 0 {
			test.Error("plugins is empty!")
		}
		if !reflect.DeepEqual(testcase.Want, plugins) {
			test.Errorf("expected %v, got %v", testcase.Want, plugins)
		}
	}
}

func equal(a, b []PluginDesc) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
