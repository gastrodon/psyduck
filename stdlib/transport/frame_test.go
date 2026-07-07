package transport

import (
	"bytes"
	"strings"
	"testing"
)

func collect(t *testing.T, d Delimit, in string) []string {
	t.Helper()
	var got []string
	if err := d.Split(strings.NewReader(in), func(b []byte) error {
		got = append(got, string(b))
		return nil
	}); err != nil {
		t.Fatalf("split: %v", err)
	}
	return got
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSplitNewline(t *testing.T) {
	d := Delimit{Sep: "\n", SepByte: -1}
	if got := collect(t, d, "a\nb\nc"); !eq(got, []string{"a", "b", "c"}) {
		t.Errorf("newline split = %v", got)
	}
	// no trailing separator still yields the last piece
	if got := collect(t, d, "a\nb\n"); !eq(got, []string{"a", "b"}) {
		t.Errorf("trailing-sep split = %v", got)
	}
}

func TestSplitFixedSize(t *testing.T) {
	d := Delimit{SepByte: -1, SepByteIndex: 2}
	if got := collect(t, d, "abcde"); !eq(got, []string{"ab", "cd", "e"}) {
		t.Errorf("fixed-size split = %v", got)
	}
}

func TestSplitWholeStream(t *testing.T) {
	d := Delimit{Sep: "", SepByte: -1} // no framing
	if got := collect(t, d, "abc\ndef"); !eq(got, []string{"abc\ndef"}) {
		t.Errorf("whole-stream split = %v", got)
	}
}

func TestSplitGroup(t *testing.T) {
	d := Delimit{Sep: "\n", SepByte: -1, Group: 2}
	// every 2 newline-pieces are joined into one message
	if got := collect(t, d, "a\nb\nc\nd"); !eq(got, []string{"a\nb", "c\nd"}) {
		t.Errorf("group split = %v", got)
	}
}

func TestSplitEmpty(t *testing.T) {
	d := Delimit{Sep: "\n", SepByte: -1}
	if got := collect(t, d, ""); len(got) != 0 {
		t.Errorf("empty input = %v, want none", got)
	}
}

func TestSplitByte(t *testing.T) {
	d := Delimit{SepByte: 0} // NUL-delimited
	if got := collect(t, d, "a\x00b\x00c"); !eq(got, []string{"a", "b", "c"}) {
		t.Errorf("byte split = %v", got)
	}
}

func TestValidateMutualExclusion(t *testing.T) {
	if err := (Delimit{SepByte: 5, SepByteIndex: 3}).Validate(); err == nil {
		t.Error("expected error for sep-byte + sep-byte-index")
	}
	if err := (Delimit{SepByte: 300}).Validate(); err == nil {
		t.Error("expected error for out-of-range sep-byte")
	}
}

func TestJoiner(t *testing.T) {
	var buf bytes.Buffer
	d := Delimit{Sep: "\n", SepByte: -1}
	j := d.Joiner(&buf)
	for _, m := range []string{"a", "b", "c"} {
		if err := j.Write([]byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "a\nb\nc\n" {
		t.Errorf("joiner = %q", buf.String())
	}
}
