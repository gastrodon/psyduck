package parse

import (
	"math/big"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func TestParseFile(t *testing.T) {
	cases := []struct {
		literal string
		want    []*PipelineDesc
	}{
		{
			``,
			make([]*PipelineDesc, 0),
		},
		{
			`group "root" {
				produce "foo" {
					pair = {"l": 1, "r": 2}
				}

				consume "where-it-goes" {}
			}`,
			[]*PipelineDesc{{
				Name: "root",
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
			}},
		},
		{
			`group "r" {
				produce-from "r-name" {
					foo 	= "bar"
					value = 132
				}
			}`,
			[]*PipelineDesc{{
				Name: "r",
				RemoteProducers: []*MoverDesc{{
					Kind: "r-name",
					Options: map[string]cty.Value{
						"foo":   cty.StringVal("bar"),
						"value": cty.NumberVal(new(big.Float).SetUint64(132).SetPrec(512)),
					},
				}},
			}},
		},
		{
			`value {
				name = "foo"
			}

			group "scope" {
				produce "constant" {
					value = value.name
					stop-after = 30
				}

				consume "trash" {}
			}`,
			[]*PipelineDesc{{
				Name: "scope",
				Producers: []*MoverDesc{{
					Kind: "constant",
					Options: map[string]cty.Value{
						"value":      cty.StringVal("foo"),
						"stop-after": cty.NumberVal(new(big.Float).SetInt64(30).SetPrec(512)),
					},
				}},
				Consumers: []*MoverDesc{{
					Kind:    "trash",
					Options: make(map[string]cty.Value),
				}},
			}},
		},
	}

	for i, testcase := range cases {
		pipeline, err := Parse("test-literal", []byte(testcase.literal), &hcl.EvalContext{})
		if err != nil {
			t.Fatalf("test-literal[%d]: %s", i, err)
		}

		assert.ElementsMatchf(t, testcase.want, pipeline, "test-literal[%d]", i)
	}
}
