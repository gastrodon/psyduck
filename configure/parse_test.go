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
			test.Fatalf("parse-plugins-desc[%d]: failed parsing plugins [parse-plugin.psy]: %s, err!", i, diags)
		}

		if plugins == nil {
			test.Fatalf("parse-plugins-desc[%d]: failed parsing plugins: expected some plugins!", i)
		}
		if len(testcase.Want) == 0 {
			test.Fatalf("parse-plugins-desc[%d]: failed parsing plugins: expected plugins, got empty!", i)
		}
		if !reflect.DeepEqual(testcase.Want, plugins) {
			test.Fatalf("parse-plugins-desc[%d]: failed parsing plugins: expected %v, got %v!", i, testcase.Want, plugins)
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
