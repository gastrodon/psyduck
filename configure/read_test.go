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
			test.Fatalf("test-literal[%d]: %s", i, err)
		}

		if len(testcase.Want) != len(configs) {
			test.Errorf("expected %d configs, got %d", len(testcase.Want), len(configs))
		}
		for name, pipeline := range testcase.Want {
			if pipeline.Name != configs[name].Name {
				test.Errorf("for %s, expected name %s, got %s", name, pipeline.Name, configs[name].Name)
			}

			if len(pipeline.Producers) != len(configs[name].Producers) {
				test.Errorf("for %s, expected %d producers, got %d", name, len(pipeline.Producers), len(configs[name].Producers))
			}
			if len(pipeline.Consumers) != len(configs[name].Consumers) {
				test.Errorf("for %s, expected %d consumers, got %d", name, len(pipeline.Consumers), len(configs[name].Consumers))
			}
			if len(pipeline.Transformers) != len(configs[name].Transformers) {
				test.Errorf("for %s, expected %d transformers, got %d", name, len(pipeline.Transformers), len(configs[name].Transformers))
			}
		}
	}
}
