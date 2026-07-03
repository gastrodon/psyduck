package main

import (
	"testing"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/parse/hcl"
	"github.com/gastrodon/psyduck/stdlib"
)

func TestDocsHCLParses(t *testing.T) {
	sources, err := parse.SourceFromDir("docs/hcl")
	if err != nil {
		t.Fatal(err)
	}

	amqpSpec := []*sdk.Spec{
		{Name: "connection", Type: sdk.TypeString, Required: true},
		{Name: "queue", Type: sdk.TypeString, Required: true},
	}
	fakeAmqp := sdk.NewInProc("amqp", &sdk.Resource{
		Name:  "amqp-queue",
		Kinds: sdk.PRODUCER | sdk.CONSUMER,
		ProvideProducer: func(sdk.Parser) (sdk.Producer, error) { return nil, nil },
		ProvideConsumer: func(sdk.Parser) (sdk.Consumer, error) { return nil, nil },
		Spec: amqpSpec,
	})

	pipelines, err := hcl.NewParserHCL().Parse(sources, []sdk.Plugin{stdlib.Plugin(), fakeAmqp})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"1-to-1", "1-to-many", "many-to-1", "many-to-many",
		"locals", "load-left", "move-right",
		"consume-remote", "ready-remote", "consume-remote-amqp"}
	for _, name := range want {
		if _, ok := pipelines[name]; !ok {
			t.Errorf("missing pipeline %q", name)
		}
	}
	if len(pipelines) != len(want) {
		t.Errorf("got %d pipelines, want %d", len(pipelines), len(want))
	}
}
