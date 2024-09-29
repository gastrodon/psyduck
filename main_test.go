package main

import (
	"fmt"
	"testing"

	"github.com/gastrodon/psyduck/parse"
	"github.com/stretchr/testify/assert"
)

func cmpPipelineDesc(t *testing.T, expected, actual *parse.PipelineDesc, title string) {
	assert.Equalf(t, expected.Name, actual.Name, "%s name", title)
	assert.ElementsMatch(t, expected.RemoteProducers, actual.RemoteProducers, "%s remote-producers", title)
	assert.ElementsMatch(t, expected.Producers, actual.Producers, "%s producers", title)
	assert.ElementsMatch(t, expected.Consumers, actual.Consumers, "%s consumers", title)
	assert.ElementsMatch(t, expected.Transformers, actual.Transformers, "%s transformers", title)
}

func TestMonifyGroup(t *testing.T) {
	cases := []struct {
		have []*parse.PipelineDesc
		want *parse.PipelineDesc
	}{
		{
			[]*parse.PipelineDesc{
				{
					Name:      "foo-bar",
					Producers: []*parse.MoverDesc{{Kind: "foo-mover"}},
				}, {
					Name:      "bar-foo",
					Consumers: []*parse.MoverDesc{{Kind: "foo-consumer"}},
				},
			},
			&parse.PipelineDesc{
				Producers: []*parse.MoverDesc{{Kind: "foo-mover"}},
				Consumers: []*parse.MoverDesc{{Kind: "foo-consumer"}},
			},
		},
		{
			[]*parse.PipelineDesc{
				{
					Producers: []*parse.MoverDesc{{Kind: "lecks"}},
				},
				{
					Producers: []*parse.MoverDesc{{Kind: "reichs"}},
				},
			},
			&parse.PipelineDesc{
				Producers: []*parse.MoverDesc{{Kind: "lecks"}, {Kind: "reichs"}},
			},
		},
	}

	for i, testcase := range cases {
		cmpPipelineDesc(t, testcase.want, MonifyGroup(testcase.have), fmt.Sprintf("test-monify-group[%d]", i))
	}
}
