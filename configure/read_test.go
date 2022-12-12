package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var cases = []struct {
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
				Producers:    []*Resource{{Kind: "test", Name: "p"}},
				Consumers:    []*Resource{{Kind: "test", Name: "c"}},
				Transformers: nil,
			},
		},
	},
}

func TestLiteral(test *testing.T) {
	for _, testcase := range cases {
		configs, _, err := Literal(testcase.Filename, []byte(testcase.Literal))
		if err != nil {
			test.Fatal(err)
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
