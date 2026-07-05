package transform

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/data"
)

// codecTransformer is the shared skeleton for every codec-aware transformer.
// It decodes the input per `decode`, runs op over the resulting Value, and
// encodes the result per `encode`. op returning a nil Value drops the message.
// onError decides what happens when any stage fails; a nil onError defaults
// to data.Propagate.
func codecTransformer(decode, encode string, onError data.OnError, op func(data.Value) (data.Value, error)) sdk.Transformer {
	if decode == "" {
		decode = "json"
	}
	if encode == "" {
		encode = decode
	}
	if onError == nil {
		onError = data.Propagate
	}

	fail := func(err error) ([]byte, error) { return nil, onError(err) }

	return func(in []byte) ([]byte, error) {
		v, err := data.Decode(in, decode)
		if err != nil {
			return fail(err)
		}
		out, err := op(v)
		if err != nil {
			return fail(err)
		}
		if out == nil {
			return nil, nil // op chose to drop
		}
		b, err := data.Encode(out, encode)
		if err != nil {
			return fail(err)
		}
		return b, nil
	}
}
