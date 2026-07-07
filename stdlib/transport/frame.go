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
// joined back (write). The separator forms are resolved by precedence —
// sep-byte-index, then sep-byte, then sep — so setting a higher-precedence
// form overrides a lower one. group batches that many pieces into one message.
type Delimit struct {
	Sep          string `psy:"sep"`            // string separator; default "\n"
	SepByte      int    `psy:"sep-byte"`       // single byte 0..255; -1 = unset
	SepByteIndex int    `psy:"sep-byte-index"` // fixed chunk size in bytes; 0 = unset
	Group        int    `psy:"group"`          // pieces per emitted message; 0/1 = one
}

// Validate rejects incompatible separator combinations and out-of-range bytes.
func (d Delimit) Validate() error {
	if d.SepByte >= 0 && d.SepByteIndex > 0 {
		return fmt.Errorf("sep-byte and sep-byte-index are mutually exclusive")
	}
	if d.SepByte > 255 {
		return fmt.Errorf("sep-byte must be 0..255, got %d", d.SepByte)
	}
	return nil
}

// sepBytes returns the separator bytes and whether the stream is delimited at
// all. Fixed-size (sep-byte-index) framing has no separator bytes and is
// reported separately via fixedSize.
func (d Delimit) sepBytes() ([]byte, bool) {
	switch {
	case d.SepByteIndex > 0:
		return nil, false
	case d.SepByte >= 0:
		return []byte{byte(d.SepByte)}, true
	case d.Sep != "":
		return []byte(d.Sep), true
	default:
		return nil, false
	}
}

func (d Delimit) group() int {
	if d.Group <= 0 {
		return 1
	}
	return d.Group
}

// Split reads r, cuts it into messages per the separator/group rules, and
// calls emit once per message. A stream with no separator and no fixed size is
// emitted whole as a single message.
func (d Delimit) Split(r io.Reader, emit func([]byte) error) error {
	sep, delimited := d.sepBytes()
	if !delimited && d.SepByteIndex <= 0 {
		all, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		return emit(all)
	}

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
	if d.SepByteIndex > 0 {
		n := d.SepByteIndex
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

// Joiner builds a write-side joiner over w.
func (d Delimit) Joiner(w io.Writer) *Joiner {
	sep, _ := d.sepBytes()
	return &Joiner{w: w, sep: sep, group: d.group()}
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
