package produce

import (
	"bufio"
	"os"

	"github.com/psyduck-etl/sdk"
)

type stdinConfig struct {
	SkipEmpty bool `psy:"skip-empty"`
}

func Stdin(parse sdk.Parser) (sdk.Producer, error) {
	config := new(stdinConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Bytes()
			if config.SkipEmpty && len(line) == 0 {
				continue
			}
			cp := make([]byte, len(line))
			copy(cp, line)
			send <- cp
		}

		if err := scanner.Err(); err != nil {
			errs <- err
		}
	}, nil
}
