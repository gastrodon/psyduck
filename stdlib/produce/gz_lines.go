package produce

import (
	"bufio"
	"compress/gzip"
	"os"

	"github.com/psyduck-etl/sdk"
)

type gzLinesConfig struct {
	Path      string `psy:"path"`
	SkipEmpty bool   `psy:"skip-empty"`
}

func GzLines(parse sdk.Parser) (sdk.Producer, error) {
	config := new(gzLinesConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		f, err := os.Open(config.Path)
		if err != nil {
			errs <- err
			return
		}
		defer f.Close()

		gr, err := gzip.NewReader(f)
		if err != nil {
			errs <- err
			return
		}
		defer gr.Close()

		scanner := bufio.NewScanner(gr)
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
