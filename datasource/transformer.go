package datasource

import "github.com/psyduck-etl/sdk"

// ComposeTransformers chains transformers in order. A nil return (filter)
// short-circuits the chain.
func ComposeTransformers(ts ...sdk.Transformer) sdk.Transformer {
	switch len(ts) {
	case 0:
		return func(data []byte) ([]byte, error) { return data, nil }
	case 1:
		return ts[0]
	default:
		return func(data []byte) ([]byte, error) {
			for _, t := range ts {
				var err error
				data, err = t(data)
				if err != nil || data == nil {
					return nil, err
				}
			}
			return data, nil
		}
	}
}
