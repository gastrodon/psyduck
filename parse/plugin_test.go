package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
