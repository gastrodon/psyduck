package configure

import (
	"testing"
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
			test.Fatalf("literal[%d]: failed reading literal [%s]: %s, err!", i, testcase.Filename, err)
		}

		if len(testcase.Want) != len(configs) {
			test.Fatalf("literal[%d]: failed reading literal: expected %d configs, got %d!", i, len(testcase.Want), len(configs))
		}
		for name, pipeline := range testcase.Want {
			if pipeline.Name != configs[name].Name {
				test.Fatalf("literal[%d]: failed reading literal for %s: expected name %s, got %s!", i, name, pipeline.Name, configs[name].Name)
			}

			if len(pipeline.Producers) != len(configs[name].Producers) {
				test.Fatalf("literal[%d]: failed reading literal for %s: expected %d producers, got %d!", i, name, len(pipeline.Producers), len(configs[name].Producers))
			}
			if len(pipeline.Consumers) != len(configs[name].Consumers) {
				test.Fatalf("literal[%d]: failed reading literal for %s: expected %d consumers, got %d!", i, name, len(pipeline.Consumers), len(configs[name].Consumers))
			}
			if len(pipeline.Transformers) != len(configs[name].Transformers) {
				test.Fatalf("literal[%d]: failed reading literal for %s: expected %d transformers, got %d!", i, name, len(pipeline.Transformers), len(configs[name].Transformers))
			}
		}
	}
}
