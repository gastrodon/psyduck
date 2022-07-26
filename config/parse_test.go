package config

import (
	"testing"
)

var cases = []struct {
	Source PipelineRaw
	Want   PipelineDescriptor
}{
	{
		Source: PipelineRaw{
			Producers: []map[string]interface{}{
				map[string]interface{}{
					"kind":  "std-constant",
					"value": "0",
				},
			},
			Consumers: []map[string]interface{}{
				map[string]interface{}{
					"kind": "std-trash",
				},
			},
			Transformers: []map[string]interface{}{
				map[string]interface{}{
					"kind": "std-inspect",
				},
			},
		},
		Want: PipelineDescriptor{
			Producers: []*Descriptor{
				&Descriptor{
					Kind:   "std-constant",
					Config: map[string]interface{}{"value": "0"},
				},
			},
			Consumers: []*Descriptor{
				&Descriptor{
					Kind:   "std-trash",
					Config: nil,
				},
			},
			Transformers: []*Descriptor{
				&Descriptor{
					Kind:   "std-inspect",
					Config: nil,
				},
			},
		},
	},
}

func Test_makePipelineDescriptor(test *testing.T) {
	for _, testcase := range cases {
		if _, err := makePipelineDescriptor(testcase.Source); err != nil {
			test.Fatal(err)
		}

	}
}
