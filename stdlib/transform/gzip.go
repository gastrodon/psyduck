package transform

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/psyduck-etl/sdk"
)

type gzipCompressConfig struct {
	Level int `psy:"level"`
}

func GzipCompress(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(gzipCompressConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	level := config.Level
	if level == 0 {
		level = gzip.DefaultCompression
	}

	return func(in []byte) ([]byte, error) {
		var buf bytes.Buffer
		w, err := gzip.NewWriterLevel(&buf, level)
		if err != nil {
			return nil, fmt.Errorf("gzip-compress: %w", err)
		}
		if _, err := w.Write(in); err != nil {
			return nil, fmt.Errorf("gzip-compress write: %w", err)
		}
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("gzip-compress close: %w", err)
		}
		return buf.Bytes(), nil
	}, nil
}

func GzipDecompress(parse sdk.Parser) (sdk.Transformer, error) {
	return func(in []byte) ([]byte, error) {
		r, err := gzip.NewReader(bytes.NewReader(in))
		if err != nil {
			return nil, fmt.Errorf("gzip-decompress: %w", err)
		}
		defer r.Close()

		out, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("gzip-decompress read: %w", err)
		}
		return out, nil
	}, nil
}
