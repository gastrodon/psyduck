package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLiteral(test *testing.T) {
	cases := []struct {
		Literal  string
		Filename string
		Want     map[string]*Pipeline
	}{
		{
			Literal: `
			produce "test" "p" {}
			consume "test" "c" {}
			pipeline "test" {
				produce = [produce.test.p]
				consume = [consume.test.c]
				transform = []
			}
			`,
			Filename: "test.psy",
			Want: map[string]*Pipeline{
				"test": {
					Name:         "test",
					Producers:    []*pipelinePart{{Kind: "test", Name: "p"}},
					Consumers:    []*pipelinePart{{Kind: "test", Name: "c"}},
					Transformers: nil,
				},
			},
		},
	}

	for i, testcase := range cases {
		configs, _, err := Literal(testcase.Filename, []byte(testcase.Literal))
		if err != nil {
			test.Fatalf("test-literal[%d]: %s", i, err)
		}

		assert.Equal(test, len(testcase.Want), len(configs))
		for name, pipeline := range testcase.Want {
			assert.Equal(test, pipeline.Name, configs[name].Name)

			assert.Equal(test, len(pipeline.Producers), len(configs[name].Producers))
			assert.Equal(test, len(pipeline.Consumers), len(configs[name].Consumers))
			assert.Equal(test, len(pipeline.Transformers), len(configs[name].Transformers))
		}
	}
}
