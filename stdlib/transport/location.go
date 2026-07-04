package transport

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// A location is a URI or path that names where bytes come from or go to. The
// scheme dispatches the transport:
//
//	(none) / file://   local file      "-" stdin/stdout, "--" stderr
//	tcp://host:port     TCP connection
//	udp://host:port     UDP connection
//	unix:///path        unix-domain socket
//
// Read and write share the same scheme table so a location reads the way it
// writes.

// scheme splits a location into its scheme and the remainder.
func scheme(location string) (string, string) {
	if i := strings.Index(location, "://"); i >= 0 {
		return location[:i], location[i+3:]
	}
	return "", location
}

// OpenReader opens location for reading. For sockets it dials; for files it
// opens (creating the file first when create is set). The caller closes the
// returned ReadCloser.
func OpenReader(location string, create bool) (io.ReadCloser, error) {
	if location == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	sch, rest := scheme(location)
	switch sch {
	case "", "file":
		if create {
			f, err := os.OpenFile(rest, os.O_RDONLY|os.O_CREATE, 0o644)
			if err != nil {
				return nil, err
			}
			return f, nil
		}
		return os.Open(rest)
	case "tcp", "udp":
		conn, err := net.Dial(sch, rest)
		if err != nil {
			return nil, err
		}
		return conn, nil
	case "unix":
		conn, err := net.Dial("unix", rest)
		if err != nil {
			return nil, err
		}
		return conn, nil
	default:
		return nil, fmt.Errorf("unsupported read scheme %q in %q", sch, location)
	}
}

// OpenWriter opens location for writing. Files honor append (vs truncate) and
// create; sockets dial. "-" is stdout, "--" is stderr. The caller closes the
// returned WriteCloser.
func OpenWriter(location string, appendMode, create bool) (io.WriteCloser, error) {
	switch location {
	case "-":
		return nopWriteCloser{os.Stdout}, nil
	case "--":
		return nopWriteCloser{os.Stderr}, nil
	}
	sch, rest := scheme(location)
	switch sch {
	case "", "file":
		flag := os.O_WRONLY | os.O_CREATE
		if appendMode {
			flag |= os.O_APPEND
		} else {
			flag |= os.O_TRUNC
		}
		return os.OpenFile(rest, flag, 0o644)
	case "tcp", "udp":
		return net.Dial(sch, rest)
	case "unix":
		if create {
			_ = os.Remove(rest)
		}
		return net.Dial("unix", rest)
	default:
		return nil, fmt.Errorf("unsupported write scheme %q in %q", sch, location)
	}
}

// Listen binds location and returns a net.Listener for stream schemes. For
// unix sockets, create removes a stale socket file first. UDP is handled by
// ListenPacket instead.
func Listen(location string, create bool) (net.Listener, error) {
	sch, rest := scheme(location)
	switch sch {
	case "tcp":
		return net.Listen("tcp", rest)
	case "unix":
		if create {
			_ = os.Remove(rest)
		}
		return net.Listen("unix", rest)
	default:
		return nil, fmt.Errorf("unsupported listen scheme %q in %q (use udp:// with ListenPacket)", sch, location)
	}
}

// ListenPacket binds a UDP location for datagram reading.
func ListenPacket(location string) (net.PacketConn, error) {
	sch, rest := scheme(location)
	if sch != "udp" {
		return nil, fmt.Errorf("ListenPacket wants udp://, got %q", location)
	}
	return net.ListenPacket("udp", rest)
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
