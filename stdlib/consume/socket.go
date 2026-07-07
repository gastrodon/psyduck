package consume

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

type socketConfig struct {
	Location     string `psy:"location"`
	Create       bool   `psy:"create"`
	Sep          string `psy:"sep"`
	SepByte      int    `psy:"sep-byte"`
	SepByteIndex int    `psy:"sep-byte-index"`
	Group        int    `psy:"group"`
}

// Socket dials a tcp://, udp://, or unix:// location and writes each message to
// the connection, joined by the configured separator. Consumer half of the
// dual-role `socket` resource.
func Socket(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(socketConfig)
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

		wc, err := transport.OpenWriter(config.Location, false, config.Create)
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
