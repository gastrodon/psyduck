package parse

// Source is a chunk of configuration with enough identity to attribute
// errors back to its origin. It intentionally does not commit to being a
// file: a producer that generates config dynamically yields the same type.
type Source struct {
	Name    string // "pipeline.psy", "remote://abc123", "stdin", ...
	Content []byte
}
