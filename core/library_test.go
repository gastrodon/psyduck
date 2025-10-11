package core

import (
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
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
				if resource != l.resources[resource.Name] {
					t.Fatalf("new-library[%d]: [%s.%s] failed creating library: expected %v, got %v!", i, plugin.Name, resource.Name, resource, l.resources[resource.Name])
				}
			}
		}
	}
}

func TestLibrary(t *testing.T) {
	config := map[string]interface{}{"count": 123}

	plugin := &sdk.Plugin{
		Name: "test", Resources: []*sdk.Resource{
			{
				Kinds: sdk.PRODUCER,
				Name:  "test",
				Spec: map[string]*sdk.Spec{
					"count": {Name: "count", Required: true},
				},
				ProvideProducer: func(parse sdk.Parser) (sdk.Producer, error) {
					target := new(struct {
						Count int `psy:"count"`
					})

					if err := parse(target); err != nil {
						t.Fatalf("failed parsing as provider: %s, err!", err)
					}

					if target.Count != 123 {
						t.Fatalf("failed parsing count: expected 123, got %d!", target.Count)
					}

					return func(send chan<- []byte, errs chan<- error) {
						send <- []byte{123}
					}, nil
				},
			},
		},
	}

	l := NewLibrary([]*sdk.Plugin{plugin})
	p, err := l.Producer("test", config)
	if err != nil {
		t.Fatalf("failed getting producer [test]: %s, err!", err)
	}

	send := make(chan []byte)
	go p(send, nil)

	select {
	case v := <-send:
		if v[0] != 123 {
			t.Fatalf("failed receiving value: expected 123, got %d!", v[0])
		}

		break
	case <-time.After(1 * time.Second):
		t.Fatalf("failed receiving value: expected value within 1s, got timeout!")
	}

}
