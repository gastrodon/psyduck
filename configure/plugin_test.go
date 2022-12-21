package configure

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
)

func TestReadPluginBlocks(test *testing.T) {
	cases := []struct {
		Literal  string
		Filename string
		Context  *hcl.EvalContext
		Want     *Plugins
	}{
		{
			`plugin "psyduck"  {
				source = "/std.so"
			}`,
			"main.psy",
			nil,
			&Plugins{
				Blocks: []pluginSource{
					{Name: "psyduck", Source: "/std.so"},
				},
			},
		},
	}

	for _, testcase := range cases {
		plugins, daig := readPluginBlocks(testcase.Filename, []byte(testcase.Literal), testcase.Context)
		assert.False(test, daig.HasErrors(), "%s", daig)
		assert.NotNil(test, plugins, "plugins is nil!")
		assert.Equal(test, testcase.Want, plugins)
	}
}
