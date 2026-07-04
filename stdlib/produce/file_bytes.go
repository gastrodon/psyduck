package produce

import (
	"io"
	"os"

	"github.com/psyduck-etl/sdk"
)

type fileBytesConfig struct {
	Path      string `psy:"path"`
	ChunkSize int    `psy:"chunk-size"`
}

func FileBytes(parse sdk.Parser) (sdk.Producer, error) {
	config := new(fileBytesConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.ChunkSize <= 0 {
		config.ChunkSize = 4096
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

		buf := make([]byte, config.ChunkSize)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				send <- cp
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				errs <- err
				return
			}
		}
	}, nil
}
