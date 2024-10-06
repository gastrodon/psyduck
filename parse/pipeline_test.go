package parse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
)

func drawDiags(d hcl.Diagnostics) string {
	buf := make([]string, len(d))
	for i, diag := range d {
		buf[i] = diag.Error()
	}

	return strings.Join(buf, "\n")
}

func cmpPipelineDesc(t *testing.T, expected, actual *PipelineDesc, title string) {
	assert.Equalf(t, expected.Name, actual.Name, "%s name", title)
	assert.ElementsMatch(t, expected.RemoteProducers, actual.RemoteProducers, "%s remote-producers", title)
	assert.ElementsMatch(t, expected.Producers, actual.Producers, "%s producers", title)
	assert.ElementsMatch(t, expected.Consumers, actual.Consumers, "%s consumers", title)
	assert.ElementsMatch(t, expected.Transformers, actual.Transformers, "%s transformers", title)
}

func TestMonifyGroup(t *testing.T) {
	cases := []struct {
		have GroupDesc
		want *PipelineDesc
	}{
		{
			GroupDesc{
				{
					Name:      "foo-bar",
					Producers: []*MoverDesc{{Kind: "foo-mover"}},
				}, {
					Name:      "bar-foo",
					Consumers: []*MoverDesc{{Kind: "foo-consumer"}},
				},
			},
			&PipelineDesc{
				Producers: []*MoverDesc{{Kind: "foo-mover"}},
				Consumers: []*MoverDesc{{Kind: "foo-consumer"}},
			},
		},
		{
			GroupDesc{
				{
					Producers: []*MoverDesc{{Kind: "lecks"}},
				},
				{
					Producers: []*MoverDesc{{Kind: "reichs"}},
				},
			},
			&PipelineDesc{
				Producers: []*MoverDesc{{Kind: "lecks"}, {Kind: "reichs"}},
			},
		},
	}

	for i, testcase := range cases {
		cmpPipelineDesc(t, testcase.want, testcase.have.Monify(), fmt.Sprintf("test-monify-group[%d]", i))
	}
}
