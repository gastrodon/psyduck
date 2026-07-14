package main

import (
	"context"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/rpc"
)

// main serves the plugin over gRPC to the psyduck host that launched it.
func main() { rpc.Serve(Plugin()) }

// Plugin builds the sdk.Plugin this binary serves.
func Plugin() sdk.Plugin {
	return sdk.NewInProc("example-plugin",
		&sdk.Resource{
			Name:            "constant",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: constantProducer,
			Spec: []*sdk.Spec{
				{Name: "value", Description: "string to emit", Type: sdk.TypeString, Default: ""},
				{Name: "count", Description: "number of times to emit (0 = forever)", Type: sdk.TypeInt, Default: 1},
			},
		},
	)
}

// The json tag mirrors psy so plugins/load_test.go can drive this plugin with
// a JSON-backed ConfigBlock instead of pulling in the host's HCL parser.
type constantConfig struct {
	Value string `psy:"value" json:"value"`
	Count int    `psy:"count" json:"count"`
}

func constantProducer(parse sdk.Parser) (sdk.Producer, error) {
	config := &constantConfig{}
	if err := parse(config); err != nil {
		return nil, err
	}
	value := []byte(config.Value)
	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)
		for i := 0; config.Count == 0 || i < config.Count; i++ {
			select {
			case send <- value:
				continue
			case <-ctx.Done():
				return
			}
		}
	}, nil
}
