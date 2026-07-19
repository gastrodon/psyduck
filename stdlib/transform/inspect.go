package transform

import (
	"context"
	"fmt"
	"os"

	"github.com/psyduck-etl/sdk"
)

type inspectConfig struct {
	Prefix string `psy:"prefix"`
	Output string `psy:"output"`
}

// Inspect logs each message and passes it through unchanged — the debug tap.
// Output is "stdout" (default) or "stderr"; prefix is prepended to each line.
func Inspect(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(inspectConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	out := os.Stdout
	if config.Output == "stderr" {
		out = os.Stderr
	}

	return sdk.Map(func(msg []byte) ([]byte, error) {
		fmt.Fprintf(out, "%s%s\n", config.Prefix, msg)
		return msg, nil
	}), nil
}
