package transform

import (
	"context"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"
)

// codecTransformer is the shared skeleton for every codec-aware transformer.
// It decodes the input per `decode`, runs op over the resulting Value, and
// encodes the result per `encode`. op returning a nil Value drops the message.
// onError decides what happens when any stage fails; a nil onError defaults
// to data.Raise. Like every stdlib transformer, it owns its raw channel loop
// directly — there is no shared map-one-message-to-one-message adapter
// underneath it.
func codecTransformer(decode, encode string, onError data.OnError, op func(data.Value) (data.Value, error)) sdk.Transformer {
	if decode == "" {
		decode = "json"
	}
	if encode == "" {
		encode = decode
	}
	if onError == nil {
		onError = data.Raise
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		// reportErr applies onError and, if it still wants to raise, sends
		// the result on errs. Reports whether the caller should stop (ctx
		// ended while trying to send).
		reportErr := func(err error) (stop bool) {
			if err = onError(err); err == nil {
				return false
			}
			select {
			case errs <- err:
				return false
			case <-ctx.Done():
				return true
			}
		}

		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				v, err := data.Decode(msg, decode)
				if err != nil {
					if reportErr(err) {
						return
					}
					continue
				}
				result, err := op(v)
				if err != nil {
					if reportErr(err) {
						return
					}
					continue
				}
				if result == nil {
					continue // op chose to drop
				}
				b, err := data.Encode(result, encode)
				if err != nil {
					if reportErr(err) {
						return
					}
					continue
				}
				select {
				case out <- b:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}
