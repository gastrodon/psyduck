package produce

import (
	"context"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

type socketConfig struct {
	Location     string  `psy:"location"`
	Create       bool    `psy:"create"`
	Sep          *string `psy:"sep"`
	SepByte      *int    `psy:"sep-byte"`
	SepByteIndex *int    `psy:"sep-byte-index"`
	Group        int     `psy:"group"`
}

// Socket connects to a tcp://, udp://, or unix:// location and emits framed
// messages read from the connection.
func Socket(parse sdk.Parser) (sdk.Producer, error) {
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

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		rc, err := transport.OpenReader(config.Location, config.Create)
		if err != nil {
			errs <- err
			return
		}
		defer rc.Close()

		stop := closeOnDone(ctx, rc)
		defer stop()

		if err := d.Split(rc, func(b []byte) error {
			select {
			case send <- b:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}, nil
}
