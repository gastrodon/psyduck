package configure

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

	for _, testcase := range cases {
		plugins, daig := ParsePluginsDesc("parse-plugin.psy", []byte(testcase.Literal))
		assert.False(test, daig.HasErrors(), "%s", daig)
		assert.NotNil(test, plugins, "plugins is nil!")
		assert.Equal(test, testcase.Want, plugins)
	}
}
