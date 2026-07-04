package produce

import (
	"fmt"
	"net"

	"github.com/psyduck-etl/sdk"
)

type udpListenConfig struct {
	Address string `psy:"address"`
}

// UdpListen receives UDP datagrams and emits each as a message.
func UdpListen(parse sdk.Parser) (sdk.Producer, error) {
	config := new(udpListenConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		addr, err := net.ResolveUDPAddr("udp", config.Address)
		if err != nil {
			errs <- fmt.Errorf("udp-listen resolve %s: %w", config.Address, err)
			return
		}

		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			errs <- fmt.Errorf("udp-listen bind %s: %w", config.Address, err)
			return
		}
		defer conn.Close()

		buf := make([]byte, 65536)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				errs <- fmt.Errorf("udp-listen read: %w", err)
				return
			}
			cp := make([]byte, n)
			copy(cp, buf[:n])
			send <- cp
		}
	}, nil
}
