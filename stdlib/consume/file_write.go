package consume

import (
	"os"

	"github.com/psyduck-etl/sdk"
)

type fileWriteConfig struct {
	Path      string `psy:"path"`
	Append    bool   `psy:"append"`
	Delimiter string `psy:"delimiter"`
}

func FileWrite(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(fileWriteConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Delimiter == "" {
		config.Delimiter = "\n"
	}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		flags := os.O_CREATE | os.O_WRONLY
		if config.Append {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}

		f, err := os.OpenFile(config.Path, flags, 0644)
		if err != nil {
			errs <- err
			return
		}
		defer f.Close()

		delim := []byte(config.Delimiter)
		for msg := range recv {
			if _, err := f.Write(msg); err != nil {
				errs <- err
				return
			}
			if len(delim) > 0 {
				if _, err := f.Write(delim); err != nil {
					errs <- err
					return
				}
			}
		}
	}, nil
}
