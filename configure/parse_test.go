package configure

import (
	"math/big"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
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

func TestParsePipelines(t *testing.T) {
	cases := []struct {
		literal string
		want    map[string]*PipelineDesc
		ctx     *hcl.EvalContext
	}{
		{
			`pipeline "foo" {}`,
			map[string]*PipelineDesc{"foo": {Name: "foo"}}, &hcl.EvalContext{},
		},
		{
			`pipeline "foo" {
				produce-from = {
					resource = "r-name"
					options = {
						foo = "bar"
						value = 132
					}
				}
			}`,
			map[string]*PipelineDesc{
				"foo": {
					Name: "foo",
					RemoteProducer: &MoverDesc{
						Kind: "r-name",
						Options: cty.ObjectVal(map[string]cty.Value{
							"foo":   cty.StringVal("bar"),
							"value": cty.NumberVal(new(big.Float).SetUint64(132).SetPrec(512)),
						}),
					},
				},
			},
			&hcl.EvalContext{},
		},
	}

	for i, testcase := range cases {
		pipelines, diags := ParsePipelinesDesc("parse-pipeline", []byte(testcase.literal), testcase.ctx)
		assert.Falsef(t, diags.HasErrors(), "parse-pipeline[%d]: %s", i, drawDiags(diags))

		for name, desc := range testcase.want {
			assert.Equal(t, desc, pipelines[name], "parse-pipeline[%d] pipeline[%s]", i, name)
		}
	}
}
