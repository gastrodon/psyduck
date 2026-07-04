package consume

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

type fileConfig struct {
	Location     string `psy:"location"`
	Follow       bool   `psy:"follow"` // producer-side; ignored when writing
	Append       bool   `psy:"append"`
	Create       bool   `psy:"create"`
	Sep          string `psy:"sep"`
	SepByte      int    `psy:"sep-byte"`
	SepByteIndex int    `psy:"sep-byte-index"`
	Group        int    `psy:"group"`
}

// File writes each message to a location (file path, "-" stdout, "--" stderr,
// or a socket URI), joining messages with the configured separator. It is the
// consumer half of the dual-role `file` resource — you write files the way you
// read them.
func File(parse sdk.Parser) (sdk.Consumer, error) {
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

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		wc, err := transport.OpenWriter(config.Location, config.Append, config.Create)
		if err != nil {
			errs <- err
			return
		}
		defer wc.Close()

		joiner := d.Joiner(wc)
		for msg := range recv {
			if err := joiner.Write(msg); err != nil {
				errs <- err
			}
		}
		if err := joiner.Close(); err != nil {
			errs <- err
		}
	}, nil
}
