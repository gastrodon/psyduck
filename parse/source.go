package parse

import (
	"fmt"
	"os"
	"path"
	"strings"
)

// Source is a chunk of configuration with enough identity to attribute
// errors back to its origin. It intentionally does not commit to being a
// file: a producer that generates config dynamically yields the same type.
type Source struct {
	Name    string // "pipeline.psy", "remote://abc123", "stdin", ...
	Content []byte
}

// SourceFromDir collects every .psy file in directory as its own Source,
// preserving filenames for diagnostics.
func SourceFromDir(directory string) ([]Source, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read files in %s: %w", directory, err)
	}

	sources := make([]Source, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".psy") {
			continue
		}

		content, err := os.ReadFile(path.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed reading %s: %w", entry.Name(), err)
		}

		sources = append(sources, Source{Name: entry.Name(), Content: content})
	}

	return sources, nil
}
