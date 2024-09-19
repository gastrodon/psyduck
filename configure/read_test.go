package configure

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func TestLiteral(test *testing.T) {
	cases := []struct {
		literal string
		ctx     *hcl.EvalContext
		want    map[string]*PipelineDesc
	}{
		{
			`
			pipeline "test" {
				produce = [{resource = "p", options = {}}]
				consume = [{resource = "c", options = {}}]
				transform = []
			}`,
			defaultCtx,
			map[string]*PipelineDesc{
				"test": {
					Name:         "test",
					Producers:    []*MoverDesc{{Kind: "p", Options: cty.EmptyObjectVal}},
					Consumers:    []*MoverDesc{{Kind: "c", Options: cty.EmptyObjectVal}},
					Transformers: nil,
				},
			},
		},
	}

	for i, testcase := range cases {
		configs, _, err := Literal("test-literal", []byte(testcase.literal), testcase.ctx)
		if err != nil {
			test.Fatalf("test-literal[%d]: %s", i, err)
		}

		assert.Equal(test, len(testcase.want), len(configs))
		for name, pipeline := range testcase.want {
			assert.Equal(test, pipeline.Name, configs[name].Name)

			assert.Equal(test, len(pipeline.Producers), len(configs[name].Producers))
			for idesc, desc := range pipeline.Producers {
				assert.Equal(test, desc, configs[name].Producers[i], "test-literal[%d] producer[%d]: %s", i, idesc, desc.Kind)
			}

			assert.Equal(test, len(pipeline.Consumers), len(configs[name].Consumers))
			for idesc, desc := range pipeline.Consumers {
				assert.Equal(test, desc, configs[name].Consumers[i], "test-literal[%d] consumer[%d]: %s", i, idesc, desc.Kind)
			}

			assert.Equal(test, len(pipeline.Transformers), len(configs[name].Transformers))
			for idesc, desc := range pipeline.Transformers {
				assert.Equal(test, desc, configs[name].Transformers[i], "test-literal[%d] transformer[%d]: %s", i, idesc, desc.Kind)
			}
		}
	}
}
