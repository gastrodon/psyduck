package consume

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/psyduck-etl/sdk"
)

type tarWriteConfig struct {
	Path           string `psy:"path"`
	Compression    string `psy:"compression"`
	EntryNameField string `psy:"entry-name-field"`
}

func TarWrite(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(tarWriteConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		f, err := os.Create(config.Path)
		if err != nil {
			errs <- err
			return
		}
		defer f.Close()

		var rawWriter io.Writer = f
		var gzWriter *gzip.Writer

		switch config.Compression {
		case "gz":
			gzWriter, err = gzip.NewWriterLevel(f, gzip.DefaultCompression)
			if err != nil {
				errs <- err
				return
			}
			defer gzWriter.Close()
			rawWriter = gzWriter
		case "none", "":
			// no-op
		default:
			errs <- fmt.Errorf("tar-write: unsupported compression %q (use none or gz)", config.Compression)
			return
		}

		tw := tar.NewWriter(rawWriter)
		defer tw.Close()

		seq := 0
		for msg := range recv {
			name := fmt.Sprintf("entry-%06d", seq)
			seq++

			if config.EntryNameField != "" {
				var m map[string]json.RawMessage
				if jsonErr := json.Unmarshal(msg, &m); jsonErr == nil {
					if v, ok := m[config.EntryNameField]; ok {
						var s string
						if strErr := json.Unmarshal(v, &s); strErr == nil {
							name = s
						}
					}
				}
			}

			hdr := &tar.Header{
				Name:    name,
				Mode:    0644,
				Size:    int64(len(msg)),
				ModTime: time.Now(),
			}

			if err := tw.WriteHeader(hdr); err != nil {
				errs <- err
				return
			}

			if _, err := tw.Write(msg); err != nil {
				errs <- err
				return
			}
		}
	}, nil
}
