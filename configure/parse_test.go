package configure

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
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
		assert.False(test, diags.HasErrors(), "%s", diags)
		if diags.HasErrors() {
			test.Fatalf("parse-plugin[%d] has errs: %s", i, drawDiags(diags))
		}

		assert.NotNil(test, plugins, "plugins is nil!")
		assert.NotZero(test, len(testcase.Want), "plugins is empty!")
		assert.Equal(test, testcase.Want, plugins)
	}
}
