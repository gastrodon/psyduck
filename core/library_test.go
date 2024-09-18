package core

import (
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/psyduck-etl/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func TestNewLibrary(t *testing.T) {
	cases := []struct {
		have []*sdk.Plugin
		want library
	}{
		{
			[]*sdk.Plugin{
				{Name: "psyduck", Resources: []*sdk.Resource{{Name: "test"}}},
			},
			library{map[string]*sdk.Resource{"test": {Name: "test"}}},
		},
	}

	for i, testcase := range cases {
		l := NewLibrary(testcase.have).(*library)
		for _, plugin := range testcase.have {
			for _, resource := range plugin.Resources {
				assert.Equalf(t, resource, l.resources[resource.Name], "new-library[%d] %s.%s", i, plugin.Name, resource.Name)
			}
		}
	}
}

func TestLibrary(t *testing.T) {
	have := []byte(`count = 123`)
	file, diags := hclparse.NewParser().ParseHCL(have, "test-library")
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}

	plugin := &sdk.Plugin{
		Name: "test", Resources: []*sdk.Resource{
			{
				Kinds: sdk.PRODUCER,
				Name:  "test",
				Spec: map[string]*sdk.Spec{
					"count": {Name: "count", Required: true, Type: cty.Number},
				},
				ProvideProducer: func(parse sdk.Parser) (sdk.Producer, error) {
					target := new(struct {
						Count int `psy:"count"`
					})

					if err := parse(target); err != nil {
						t.Fatalf("failed to parse as provider: %s", err)
					}

					if target.Count != 123 {
						t.Fatalf("expected count 123, got: %d", target.Count)
					}

					return func(send chan<- []byte, errs chan<- error) {
						send <- []byte{123}
					}, nil
				},
			},
		},
	}

	l := NewLibrary([]*sdk.Plugin{plugin})
	p, err := l.Producer("test", &hcl.EvalContext{}, file.Body)
	if err != nil {
		t.Fatal(err)
	}

	send := make(chan []byte)
	go p(send, nil)

	select {
	case v := <-send:
		if v[0] != 123 {
			t.Fatalf("unexpected value from send: %v", v)
		}

		break
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for send")
	}

}
