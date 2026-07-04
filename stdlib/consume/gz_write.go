package consume

import (
	"compress/gzip"
	"os"

	"github.com/psyduck-etl/sdk"
)

type gzWriteConfig struct {
	Path      string `psy:"path"`
	Level     int    `psy:"level"`
	Delimiter string `psy:"delimiter"`
}

func GzWrite(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(gzWriteConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Delimiter == "" {
		config.Delimiter = "\n"
	}

	level := config.Level
	if level == 0 {
		level = gzip.DefaultCompression
	}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		f, err := os.Create(config.Path)
		if err != nil {
			errs <- err
			return
		}
		defer f.Close()

		gw, err := gzip.NewWriterLevel(f, level)
		if err != nil {
			errs <- err
			return
		}
		defer gw.Close()

		delim := []byte(config.Delimiter)
		for msg := range recv {
			if _, err := gw.Write(msg); err != nil {
				errs <- err
				return
			}
			if len(delim) > 0 {
				if _, err := gw.Write(delim); err != nil {
					errs <- err
					return
				}
			}
		}
	}, nil
}
