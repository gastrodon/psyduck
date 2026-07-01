package datasource

// ConfigUnit represents a single loaded configuration source.
// It preserves the file identity that would otherwise be lost by concatenation.
type ConfigUnit struct {
	Name    string // original filename, e.g. "pipeline.psy"
	Content []byte // raw file content
}
