package consume

import (
	"fmt"
	"net"

	"github.com/psyduck-etl/sdk"
)

type udpWriteConfig struct {
	Address string `psy:"address"`
}

// UdpWrite sends each message as a UDP datagram to the given address.
func UdpWrite(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(udpWriteConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		addr, err := net.ResolveUDPAddr("udp", config.Address)
		if err != nil {
			errs <- fmt.Errorf("udp-write resolve %s: %w", config.Address, err)
			return
		}

		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			errs <- fmt.Errorf("udp-write dial %s: %w", config.Address, err)
			return
		}
		defer conn.Close()

		for msg := range recv {
			if _, err := conn.Write(msg); err != nil {
				errs <- fmt.Errorf("udp-write: %w", err)
				return
			}
		}
	}, nil
}
