package produce

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/psyduck-etl/sdk"
)

type tarLinesConfig struct {
	Path        string `psy:"path"`
	Match       string `psy:"match"`
	Compression string `psy:"compression"`
	SkipEmpty   bool   `psy:"skip-empty"`
}

func openTarReader(path, compression string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	if compression == "auto" {
		lower := strings.ToLower(path)
		switch {
		case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
			compression = "gz"
		case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
			compression = "bz2"
		case strings.HasSuffix(lower, ".tar.xz"):
			compression = "xz"
		default:
			compression = "none"
		}
	}

	switch compression {
	case "none", "":
		return f, nil
	case "gz":
		gr, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &multiCloser{gr, f}, nil
	case "bz2":
		return &multiCloser{io.NopCloser(bzip2.NewReader(f)), f}, nil
	default:
		f.Close()
		return nil, fmt.Errorf("unsupported tar compression: %q (use none, gz, bz2, or auto)", compression)
	}
}

// multiCloser chains two closers so that closing the outer also closes the file.
type multiCloser struct {
	io.ReadCloser
	underlying io.Closer
}

func (m *multiCloser) Close() error {
	err := m.ReadCloser.Close()
	if err2 := m.underlying.Close(); err == nil {
		err = err2
	}
	return err
}

func TarLines(parse sdk.Parser) (sdk.Producer, error) {
	config := new(tarLinesConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Match == "" {
		config.Match = "*"
	}
	if config.Compression == "" {
		config.Compression = "auto"
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		rc, err := openTarReader(config.Path, config.Compression)
		if err != nil {
			errs <- err
			return
		}
		defer rc.Close()

		tr := tar.NewReader(rc)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				errs <- err
				return
			}

			matched, err := filepath.Match(config.Match, hdr.Name)
			if err != nil {
				errs <- err
				return
			}
			if !matched {
				continue
			}

			scanner := bufio.NewScanner(tr)
			for scanner.Scan() {
				line := scanner.Bytes()
				if config.SkipEmpty && len(line) == 0 {
					continue
				}
				cp := make([]byte, len(line))
				copy(cp, line)
				send <- cp
			}

			if scanErr := scanner.Err(); scanErr != nil {
				errs <- scanErr
				return
			}
		}
	}, nil
}
