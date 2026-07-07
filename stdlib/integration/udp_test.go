package integration

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/produce"
)

// TestUDPDatagramTransport verifies the UDP listener emits one message per
// datagram with the payload intact. UDP has no connection semantics so each
// Write on the client side maps to exactly one ReadFrom on the listener side.
func TestUDPDatagramTransport(t *testing.T) {
	const N = 10
	addr := freeUDPAddr(t)
	loc := "udp://" + addr

	lp, err := produce.Listen(parser(delimitCfg(loc, false)))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	send := make(chan []byte, N)
	lerrs := make(chan error, 1)
	go lp(send, lerrs)
	drainErrs(lerrs)

	// Give the goroutine time to call net.ListenPacket and bind before we dial.
	// UDP bind is fast; 30 ms is generous.
	time.Sleep(30 * time.Millisecond)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial udp %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })

	want := make([]string, N)
	for i := 0; i < N; i++ {
		want[i] = fmt.Sprintf("dgram-%02d", i)
		if _, err := conn.Write([]byte(want[i])); err != nil {
			t.Fatalf("send datagram %d: %v", i, err)
		}
	}

	msgs := readN(t, send, N, 3*time.Second)
	assertSameSet(t, msgs, want)
	assertNoDups(t, msgs)
}
