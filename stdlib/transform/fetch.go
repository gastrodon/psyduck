package transform

import (
	"context"
	"fmt"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// fetchConfig is fetch's spec: transport.RequestSpec plus on-error, the
// knob meaningful to a per-message call rather than a poll or a post.
type fetchConfig struct {
	transport.RequestSpec
	OnError string `psy:"on-error"`
}

// Fetch performs one HTTP request per input message: each message is
// decoded as a JSON request descriptor (see transport.RequestSpec for the
// attributes it can carry) and merged over the fetch block's own spec, so
// a message only needs to set what differs from the block — anything
// neither sets falls back to transport's defaults. The response body
// becomes the transformed message, for a downstream jq/pick/pick-map stage
// to reshape.
func Fetch(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(fetchConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	ct := config.RequestSpec

	return sdk.MapContext(func(ctx context.Context, msg []byte) ([]byte, error) {
		out, err := transport.Dispatch(ctx, msg, ct)
		if err == nil {
			return out, nil
		}
		return nil, onError(fmt.Errorf("fetch: dispatch message: %w", err))
	}), nil
}
