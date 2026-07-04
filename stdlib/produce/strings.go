package produce

import "strings"

// newStringReader returns an io.Reader over a static string (used in http-poll).
func newStringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
