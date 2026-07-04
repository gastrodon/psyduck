package consume

import (
	"os"

	"github.com/psyduck-etl/sdk"
)

type stderrConfig struct {
	Delimiter string `psy:"delimiter"`
}

func Stderr(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(stderrConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Delimiter == "" {
		config.Delimiter = "\n"
	}

	delim := []byte(config.Delimiter)
	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		for msg := range recv {
			if _, err := os.Stderr.Write(msg); err != nil {
				errs <- err
				return
			}
			if len(delim) > 0 {
				if _, err := os.Stderr.Write(delim); err != nil {
					errs <- err
					return
				}
			}
		}
	}, nil
}
