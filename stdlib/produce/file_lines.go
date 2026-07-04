package produce

import (
	"bufio"
	"io"
	"os"
	"time"

	"github.com/psyduck-etl/sdk"
)

type fileLinesConfig struct {
	Path      string `psy:"path"`
	Follow    bool   `psy:"follow"`
	SkipEmpty bool   `psy:"skip-empty"`
}

func FileLines(parse sdk.Parser) (sdk.Producer, error) {
	config := new(fileLinesConfig)
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

		scanner := bufio.NewScanner(f)
		for {
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
				return
			}

			if !config.Follow {
				return
			}

			// tail mode: wait briefly then keep reading
			time.Sleep(200 * time.Millisecond)
			scanner = bufio.NewScanner(f)
			// Seek to current position (no-op) to keep reading appended data
			if _, err := f.Seek(0, io.SeekCurrent); err != nil {
				errs <- err
				return
			}
		}
	}, nil
}
