package produce

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

type fileConfig struct {
	Location     string `psy:"location"`
	Follow       bool   `psy:"follow"`
	Append       bool   `psy:"append"` // consumer-side; ignored when reading
	Create       bool   `psy:"create"`
	Sep          string `psy:"sep"`
	SepByte      int    `psy:"sep-byte"`
	SepByteIndex int    `psy:"sep-byte-index"`
	Group        int    `psy:"group"`
}

// File reads bytes from a location (file path, "-" for stdin, or a socket URI)
// and emits framed messages. With follow=true it tails a file, blocking at EOF
// and emitting new data as the file grows.
func File(parse sdk.Parser) (sdk.Producer, error) {
	config := new(fileConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	d := transport.Delimit{
		Sep:          config.Sep,
		SepByte:      config.SepByte,
		SepByteIndex: config.SepByteIndex,
		Group:        config.Group,
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		var reader io.Reader
		var closer io.Closer

		if config.Follow && isFilePath(config.Location) {
			f, err := os.Open(strings.TrimPrefix(config.Location, "file://"))
			if err != nil {
				errs <- err
				return
			}
			reader, closer = tailReader{f}, f
		} else {
			rc, err := transport.OpenReader(config.Location, config.Create)
			if err != nil {
				errs <- err
				return
			}
			reader, closer = rc, rc
		}
		defer closer.Close()

		if err := d.Split(reader, func(b []byte) error {
			send <- b
			return nil
		}); err != nil {
			errs <- err
		}
	}, nil
}

func isFilePath(location string) bool {
	sch := location
	if i := strings.Index(location, "://"); i >= 0 {
		sch = location[:i]
	} else {
		return location != "-"
	}
	return sch == "file"
}

// tailReader turns EOF into a blocking wait so a file can be followed like
// `tail -f`: on EOF it sleeps and retries rather than ending the stream.
type tailReader struct{ f *os.File }

func (t tailReader) Read(p []byte) (int, error) {
	for {
		n, err := t.f.Read(p)
		if n > 0 {
			return n, nil
		}
		if err == io.EOF {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return n, err
	}
}
