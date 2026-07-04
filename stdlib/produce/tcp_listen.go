package produce

import (
	"bufio"
	"fmt"
	"net"
	"sync"

	"github.com/psyduck-etl/sdk"
)

type tcpListenConfig struct {
	Address   string `psy:"address"`
	SkipEmpty bool   `psy:"skip-empty"`
}

// TcpListen accepts TCP connections and emits messages line-by-line from all clients.
func TcpListen(parse sdk.Parser) (sdk.Producer, error) {
	config := new(tcpListenConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		ln, err := net.Listen("tcp", config.Address)
		if err != nil {
			errs <- fmt.Errorf("tcp-listen bind %s: %w", config.Address, err)
			return
		}
		defer ln.Close()

		var wg sync.WaitGroup
		for {
			conn, err := ln.Accept()
			if err != nil {
				// listener closed — normal shutdown path
				break
			}

			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()

				scanner := bufio.NewScanner(c)
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
				}
			}(conn)
		}

		wg.Wait()
	}, nil
}
