package transform

import (
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
func Inspect(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(inspectConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	out := os.Stdout
	if config.Output == "stderr" {
		out = os.Stderr
	}

	return func(data []byte) ([]byte, bool, error) {
		fmt.Fprintf(out, "%s%s\n", config.Prefix, data)
		return data, true, nil
	}, nil
}
