// Package transport holds the shared machinery behind the stdlib transports
// (file, socket, listen, request). Framing — how a byte stream is cut into
// messages on read and joined on write — is expressed once here as Delimit and
// reused by every transport, so `sep`/`sep-byte`/`sep-byte-index`/`group`
// behave identically whether the bytes come from a file, a socket, or stdin.
package transport

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

// Delimit configures how a byte stream is split into messages (read) and
// joined back (write). Exactly one of Sep, SepByte, or SepByteIndex must be
// set — the fields are pointers so nil unambiguously means "unset" (crucially
// letting SepByte hold the legal NUL value 0). group batches that many pieces
// into one message.
type Delimit struct {
	Sep          *string `psy:"sep"`            // string separator
	SepByte      *int    `psy:"sep-byte"`       // single byte separator 0..255
	SepByteIndex *int    `psy:"sep-byte-index"` // fixed chunk size in bytes
	Group        int     `psy:"group"`          // pieces per emitted message; 0/1 = one
}

// Validate requires exactly one of Sep, SepByte, or SepByteIndex to be set
// and range-checks the numeric forms. Callers must Validate before Split or
// Joiner; downstream code assumes valid state.
func (d Delimit) Validate() error {
	set := 0
	if d.Sep != nil {
		set++
	}
	if d.SepByte != nil {
		set++
	}
	if d.SepByteIndex != nil {
		set++
	}
	if set != 1 {
		return fmt.Errorf("exactly one of sep, sep-byte, sep-byte-index must be set (got %d)", set)
	}
	if d.SepByte != nil && (*d.SepByte < 0 || *d.SepByte > 255) {
		return fmt.Errorf("sep-byte must be 0..255, got %d", *d.SepByte)
	}
	if d.SepByteIndex != nil && *d.SepByteIndex <= 0 {
		return fmt.Errorf("sep-byte-index must be > 0, got %d", *d.SepByteIndex)
	}
	return nil
}

// sepBytes returns the separator bytes for stream-delimited framing, or nil
// when framing is fixed-size (sep-byte-index). Assumes Validate has passed.
func (d Delimit) sepBytes() []byte {
	switch {
	case d.SepByteIndex != nil:
		return nil
	case d.SepByte != nil:
		return []byte{byte(*d.SepByte)}
	default:
		return []byte(*d.Sep)
	}
}

func (d Delimit) group() int {
	if d.Group <= 0 {
		return 1
	}
	return d.Group
}

// Split reads r, cuts it into messages per the separator/group rules, and
// calls emit once per message. Assumes Validate has passed.
func (d Delimit) Split(r io.Reader, emit func([]byte) error) error {
	sep := d.sepBytes()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	sc.Split(d.splitFunc(sep))

	group := d.group()
	batch := make([][]byte, 0, group)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		joined := bytes.Join(batch, sep)
		batch = batch[:0]
		return emit(joined)
	}

	for sc.Scan() {
		b := sc.Bytes()
		cp := make([]byte, len(b))
		copy(cp, b)
		batch = append(batch, cp)
		if len(batch) >= group {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

func (d Delimit) splitFunc(sep []byte) bufio.SplitFunc {
	if d.SepByteIndex != nil {
		n := *d.SepByteIndex
		return func(data []byte, atEOF bool) (int, []byte, error) {
			if atEOF && len(data) == 0 {
				return 0, nil, nil
			}
			if len(data) >= n {
				return n, data[:n], nil
			}
			if atEOF {
				return len(data), data, nil
			}
			return 0, nil, nil
		}
	}
	return func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.Index(data, sep); i >= 0 {
			return i + len(sep), data[:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
}

// Joiner writes messages to w, appending the configured separator after each
// (or each group). It is the write-side inverse of Split.
type Joiner struct {
	w     io.Writer
	sep   []byte
	group int
	batch [][]byte
}

// Joiner builds a write-side joiner over w. Assumes Validate has passed.
func (d Delimit) Joiner(w io.Writer) *Joiner {
	return &Joiner{w: w, sep: d.sepBytes(), group: d.group()}
}

// Write buffers msg, flushing a joined batch once group messages accumulate.
func (j *Joiner) Write(msg []byte) error {
	cp := make([]byte, len(msg))
	copy(cp, msg)
	j.batch = append(j.batch, cp)
	if len(j.batch) >= j.group {
		return j.flush()
	}
	return nil
}

func (j *Joiner) flush() error {
	if len(j.batch) == 0 {
		return nil
	}
	joined := bytes.Join(j.batch, j.sep)
	j.batch = j.batch[:0]
	if _, err := j.w.Write(joined); err != nil {
		return err
	}
	if len(j.sep) > 0 {
		if _, err := j.w.Write(j.sep); err != nil {
			return err
		}
	}
	return nil
}

// Close flushes any buffered partial group.
func (j *Joiner) Close() error { return j.flush() }
