package configure

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func cmpPipelineDesc(t *testing.T, expected, actual *PipelineDesc, title string) {
	assert.ElementsMatch(t, expected.RemoteProducers, actual.RemoteProducers, "%s remote-producers", title)
	assert.ElementsMatch(t, expected.Producers, actual.Producers, "%s producers", title)
	assert.ElementsMatch(t, expected.Consumers, actual.Consumers, "%s consumers", title)
	assert.ElementsMatch(t, expected.Transformers, actual.Transformers, "%s transformers", title)
	assert.Equal(t, expected.StopAfter, actual.StopAfter, "%s", title)
	assert.Equal(t, expected.ExitOnError, actual.ExitOnError, "%s", title)
}

func TestLiteral(t *testing.T) {
	cases := []struct {
		literal string
		want    *PipelineDesc
	}{
		{
			`produce "p" {}

			consume "c" {}`,
			&PipelineDesc{
				RemoteProducers: make([]*MoverDesc, 0),
				Producers:       []*MoverDesc{{Kind: "p", Options: make(map[string]cty.Value)}},
				Consumers:       []*MoverDesc{{Kind: "c", Options: make(map[string]cty.Value)}},
				Transformers:    make([]*MoverDesc, 0),
			},
		},
		{
			`value {
				name = "foo"
			}

			produce "constant" {
				value = value.name
				stop-after = 30
			}

			consume "trash" {}`,
			&PipelineDesc{
				RemoteProducers: make([]*MoverDesc, 0),
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
				Transformers: make([]*MoverDesc, 0),
				StopAfter:    0,
				ExitOnError:  false,
			},
		},
	}

	for i, testcase := range cases {
		pipeline, err := Literal("test-literal", []byte(testcase.literal), &hcl.EvalContext{})
		if err != nil {
			t.Fatalf("test-literal[%d]: %s", i, err)
		}

		cmpPipelineDesc(t, testcase.want, pipeline, fmt.Sprintf("test-literal[%d]", i))
	}
}

func TestLiteralGroup(t *testing.T) {
	cases := []struct {
		files map[string][]byte
		want  *PipelineDesc
	}{
		{map[string][]byte{
			"foo": []byte(`produce "foo" {}`),
			"food": []byte(`
				produce "fooding" {}

				produce "eating" {}

				consume "foo-consume" {}
			`),
		}, &PipelineDesc{
			Producers: []*MoverDesc{{"foo", make(map[string]cty.Value)}, {"fooding", make(map[string]cty.Value)}, {"eating", make(map[string]cty.Value)}},
			Consumers: []*MoverDesc{{"foo-consume", make(map[string]cty.Value)}},
		},
		},
	}

	for i, testcase := range cases {
		pipeline, diags := LiteralGroup(testcase.files, &hcl.EvalContext{})
		if diags.HasErrors() {
			t.Fatalf("test-literal-group[%d]: %s", i, diags)
		}

		cmpPipelineDesc(t, testcase.want, pipeline, fmt.Sprintf("test-literal-group[%d]", i))
	}
}
