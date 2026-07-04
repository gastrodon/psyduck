package consume

import (
	"os"

	"github.com/psyduck-etl/sdk"
)

type stdoutConfig struct {
	Delimiter string `psy:"delimiter"`
}

func Stdout(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(stdoutConfig)
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
			if _, err := os.Stdout.Write(msg); err != nil {
				errs <- err
				return
			}
			if len(delim) > 0 {
				if _, err := os.Stdout.Write(delim); err != nil {
					errs <- err
					return
				}
			}
		}
	}, nil
}
