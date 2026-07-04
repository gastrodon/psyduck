package consume

import (
	"fmt"
	"net"

	"github.com/psyduck-etl/sdk"
)

type tcpWriteConfig struct {
	Address   string `psy:"address"`
	Delimiter string `psy:"delimiter"`
}

// TcpWrite dials a TCP server and writes each message followed by the delimiter.
func TcpWrite(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(tcpWriteConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Delimiter == "" {
		config.Delimiter = "\n"
	}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		conn, err := net.Dial("tcp", config.Address)
		if err != nil {
			errs <- fmt.Errorf("tcp-write dial %s: %w", config.Address, err)
			return
		}
		defer conn.Close()

		delim := []byte(config.Delimiter)
		for msg := range recv {
			if _, err := conn.Write(msg); err != nil {
				errs <- fmt.Errorf("tcp-write: %w", err)
				return
			}
			if len(delim) > 0 {
				if _, err := conn.Write(delim); err != nil {
					errs <- fmt.Errorf("tcp-write delimiter: %w", err)
					return
				}
			}
		}
	}, nil
}
