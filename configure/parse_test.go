package configure

import (
	"fmt"
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
		want    *PipelineDesc
	}{
		{
			``,
			&PipelineDesc{
				RemoteProducers: make([]*MoverDesc, 0),
			},
		},
		{
			`produce "foo" {
				pair = {"l": 1, "r": 2}
			}

			consume "where-it-goes" {}`,
			&PipelineDesc{
				RemoteProducers: make([]*MoverDesc, 0),
				Producers: []*MoverDesc{{
					Kind: "foo",
					Options: map[string]cty.Value{
						"pair": cty.ObjectVal(map[string]cty.Value{
							"l": cty.NumberVal(new(big.Float).SetUint64(1).SetPrec(512)),
							"r": cty.NumberVal(new(big.Float).SetUint64(2).SetPrec(512)),
						}),
					},
				}},
				Consumers: []*MoverDesc{{
					Kind:    "where-it-goes",
					Options: make(map[string]cty.Value),
				}},
				Transformers: make([]*MoverDesc, 0),
				StopAfter:    0,
				ExitOnError:  false,
			},
		},
		{
			`produce-from "r-name" {
				foo 	= "bar"
				value = 132
			}`,
			&PipelineDesc{
				RemoteProducers: []*MoverDesc{{
					Kind: "r-name",
					Options: map[string]cty.Value{
						"foo":   cty.StringVal("bar"),
						"value": cty.NumberVal(new(big.Float).SetUint64(132).SetPrec(512)),
					},
				}},
				Producers:    make([]*MoverDesc, 0),
				Consumers:    make([]*MoverDesc, 0),
				Transformers: make([]*MoverDesc, 0),
			},
		},
	}
	for i, testcase := range cases {
		pipeline, diags := ParsePipelinesDesc("test-parse-pipeline", []byte(testcase.literal), &hcl.EvalContext{})
		if diags.HasErrors() {
			t.Fatalf("test-parse-pipeline[%d]: %s", i, diags)
		}

		cmpPipelineDesc(t, testcase.want, pipeline, fmt.Sprintf("test-parse-pipeline[%d]", i))
	}
}
