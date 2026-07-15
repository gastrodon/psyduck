package transform

import (
	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"
)

// mapErr lifts a per-message func onto the Transformer contract via sdk.Map,
// routing failures through onError first: a raised error is forwarded on errs
// and the message dropped, while a swallowed one (onError returns nil) drops
// the message silently. A (nil, nil) return from fn is a plain drop. This is
// the shared skeleton for every codec-aware transformer that carries an
// on-error policy.
func mapErr(onError data.OnError, fn func([]byte) ([]byte, error)) sdk.Transformer {
	if onError == nil {
		onError = data.Raise
	}
	return sdk.Map(func(msg []byte) ([]byte, error) {
		out, err := fn(msg)
		if err == nil {
			return out, nil
		}
		if err = onError(err); err != nil {
			return nil, err
		}
		return nil, nil
	})
}

// codecTransformer is the shared skeleton for every codec-aware transformer.
// It decodes the input per `decode`, runs op over the resulting Value, and
// encodes the result per `encode`. op returning a nil Value drops the message.
// onError decides what happens when any stage fails; a nil onError defaults
// to data.Raise.
func codecTransformer(decode, encode string, onError data.OnError, op func(data.Value) (data.Value, error)) sdk.Transformer {
	if decode == "" {
		decode = "json"
	}
	if encode == "" {
		encode = decode
	}

	return mapErr(onError, func(msg []byte) ([]byte, error) {
		v, err := data.Decode(msg, decode)
		if err != nil {
			return nil, err
		}
		result, err := op(v)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil // op chose to drop
		}
		return data.Encode(result, encode)
	})
}
