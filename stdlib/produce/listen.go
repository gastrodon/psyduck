package produce

import (
	"net"
	"strings"
	"sync"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

type listenConfig struct {
	Location     string  `psy:"location"`
	Create       bool    `psy:"create"`
	Sep          *string `psy:"sep"`
	SepByte      *int    `psy:"sep-byte"`
	SepByteIndex *int    `psy:"sep-byte-index"`
	Group        int     `psy:"group"`
}

// Listen binds a location and emits framed messages read from every accepted
// connection. TCP and unix sockets accept a stream of connections, each framed
// independently and merged into the output; udp:// reads datagrams. This is a
// natural sink for the socket→meta-producer pattern: many writers, one reader.
func Listen(parse sdk.Parser) (sdk.Producer, error) {
	config := new(listenConfig)
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

	if strings.HasPrefix(config.Location, "udp://") {
		return listenPacket(config.Location, d), nil
	}
	return listenStream(config.Location, config.Create, d), nil
}

func listenStream(location string, create bool, d transport.Delimit) sdk.Producer {
	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		ln, err := transport.Listen(location, create)
		if err != nil {
			errs <- err
			return
		}
		defer ln.Close()

		var wg sync.WaitGroup
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				_ = d.Split(c, func(b []byte) error {
					send <- b
					return nil
				})
			}(conn)
		}
		wg.Wait()
	}
}

func listenPacket(location string, d transport.Delimit) sdk.Producer {
	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		pc, err := transport.ListenPacket(location)
		if err != nil {
			errs <- err
			return
		}
		defer pc.Close()

		buf := make([]byte, 64*1024)
		for {
			n, _, err := pc.ReadFrom(buf)
			if err != nil {
				break
			}
			msg := make([]byte, n)
			copy(msg, buf[:n])
			send <- msg
		}
	}
}
