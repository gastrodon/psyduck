package bench

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// newlineStream renders `lines` newline-delimited records, standing in for
// a chunk of an NDJSON-ish log file read by the `file`/`socket` transports.
func newlineStream(lines int) []byte {
	var buf bytes.Buffer
	for i := 0; i < lines; i++ {
		buf.WriteString("line-")
		buf.WriteString(strconv.Itoa(i))
		buf.WriteString("-payload-data")
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// BenchmarkTransportSplit measures transport.Delimit.Split -- the framing
// layer shared by every stdlib transport (file, socket, listen, request) --
// under stream-separator and fixed-size-record framing.
func BenchmarkTransportSplit(b *testing.B) {
	stream := newlineStream(5000)
	sep := "\n"
	d := transport.Delimit{Sep: &sep}
	if err := d.Validate(); err != nil {
		b.Fatal(err)
	}

	b.Run("sep-newline/5000-lines", func(b *testing.B) {
		report(b, len(stream))
		for i := 0; i < b.N; i++ {
			if err := d.Split(bytes.NewReader(stream), func([]byte) error { return nil }); err != nil {
				b.Fatal(err)
			}
		}
	})

	recSize := 32
	fixedStream := deterministicBytes(5000 * recSize)
	dFixed := transport.Delimit{SepByteIndex: &recSize}
	if err := dFixed.Validate(); err != nil {
		b.Fatal(err)
	}

	b.Run("sep-byte-index-32B/5000-records", func(b *testing.B) {
		report(b, len(fixedStream))
		for i := 0; i < b.N; i++ {
			if err := dFixed.Split(bytes.NewReader(fixedStream), func([]byte) error { return nil }); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("sep-newline/grouped-64", func(b *testing.B) {
		dGroup := transport.Delimit{Sep: &sep, Group: 64}
		if err := dGroup.Validate(); err != nil {
			b.Fatal(err)
		}
		report(b, len(stream))
		for i := 0; i < b.N; i++ {
			if err := dGroup.Split(bytes.NewReader(stream), func([]byte) error { return nil }); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkTransportJoiner measures the write-side inverse of Split.
func BenchmarkTransportJoiner(b *testing.B) {
	sep := "\n"
	d := transport.Delimit{Sep: &sep}
	msg := payloadSmall

	b.Run("write/no-group", func(b *testing.B) {
		var buf bytes.Buffer
		j := d.Joiner(&buf)
		report(b, len(msg))
		for i := 0; i < b.N; i++ {
			buf.Reset()
			if err := j.Write(msg); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("write/group-64", func(b *testing.B) {
		dGroup := transport.Delimit{Sep: &sep, Group: 64}
		var buf bytes.Buffer
		j := dGroup.Joiner(&buf)
		report(b, len(msg))
		for i := 0; i < b.N; i++ {
			buf.Reset()
			if err := j.Write(msg); err != nil {
				b.Fatal(err)
			}
		}
	})
}
