package transform

import (
	"encoding/base64"
	"fmt"

	"github.com/psyduck-etl/sdk"
)

type base64Config struct {
	Encoding string `psy:"encoding"`
}

func selectEncoding(name string) (*base64.Encoding, error) {
	switch name {
	case "std", "":
		return base64.StdEncoding, nil
	case "url":
		return base64.URLEncoding, nil
	case "raw-std":
		return base64.RawStdEncoding, nil
	case "raw-url":
		return base64.RawURLEncoding, nil
	default:
		return nil, fmt.Errorf("base64: unknown encoding %q (use std, url, raw-std, or raw-url)", name)
	}
}

func Base64Encode(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(base64Config)
	if err := parse(config); err != nil {
		return nil, err
	}

	enc, err := selectEncoding(config.Encoding)
	if err != nil {
		return nil, err
	}

	return func(in []byte) ([]byte, error) {
		out := make([]byte, enc.EncodedLen(len(in)))
		enc.Encode(out, in)
		return out, nil
	}, nil
}

func Base64Decode(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(base64Config)
	if err := parse(config); err != nil {
		return nil, err
	}

	enc, err := selectEncoding(config.Encoding)
	if err != nil {
		return nil, err
	}

	return func(in []byte) ([]byte, error) {
		out := make([]byte, enc.DecodedLen(len(in)))
		n, err := enc.Decode(out, in)
		if err != nil {
			return nil, fmt.Errorf("base64-decode: %w", err)
		}
		return out[:n], nil
	}, nil
}
