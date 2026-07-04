package produce

import (
	"archive/zip"
	"bufio"
	"path/filepath"

	"github.com/psyduck-etl/sdk"
)

type zipLinesConfig struct {
	Path      string `psy:"path"`
	Match     string `psy:"match"`
	SkipEmpty bool   `psy:"skip-empty"`
}

func ZipLines(parse sdk.Parser) (sdk.Producer, error) {
	config := new(zipLinesConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Match == "" {
		config.Match = "*"
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		r, err := zip.OpenReader(config.Path)
		if err != nil {
			errs <- err
			return
		}
		defer r.Close()

		for _, f := range r.File {
			matched, err := filepath.Match(config.Match, f.Name)
			if err != nil {
				errs <- err
				return
			}
			if !matched {
				continue
			}

			rc, err := f.Open()
			if err != nil {
				errs <- err
				return
			}

			scanner := bufio.NewScanner(rc)
			for scanner.Scan() {
				line := scanner.Bytes()
				if config.SkipEmpty && len(line) == 0 {
					continue
				}
				cp := make([]byte, len(line))
				copy(cp, line)
				send <- cp
			}

			scanErr := scanner.Err()
			rc.Close()
			if scanErr != nil {
				errs <- scanErr
				return
			}
		}
	}, nil
}
