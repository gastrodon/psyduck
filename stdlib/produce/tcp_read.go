package produce

import (
	"bufio"
	"fmt"
	"net"

	"github.com/psyduck-etl/sdk"
)

type tcpReadConfig struct {
	Address   string `psy:"address"`
	SkipEmpty bool   `psy:"skip-empty"`
}

// TcpRead dials a TCP server and emits messages line-by-line.
func TcpRead(parse sdk.Parser) (sdk.Producer, error) {
	config := new(tcpReadConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		conn, err := net.Dial("tcp", config.Address)
		if err != nil {
			errs <- fmt.Errorf("tcp-read dial %s: %w", config.Address, err)
			return
		}
		defer conn.Close()

		scanner := bufio.NewScanner(conn)
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
		}
	}, nil
}
