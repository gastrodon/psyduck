// Package configure gathers configuration sources from disk. Parsing lives
// in format implementations (see configure/hcl).
package configure

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/gastrodon/psyduck/parse"
)

// ReadFiles collects every .psy file in directory as its own parse.Source,
// preserving filenames for diagnostics.
func ReadFiles(directory string) ([]parse.Source, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read files in %s: %w", directory, err)
	}

	sources := make([]parse.Source, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".psy") {
			continue
		}

		content, err := os.ReadFile(path.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed reading %s: %w", entry.Name(), err)
		}

		sources = append(sources, parse.Source{Name: entry.Name(), Content: content})
	}

	return sources, nil
}
