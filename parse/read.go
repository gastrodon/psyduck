package parse

import (
	"fmt"
	"os"
	"path"
	"strings"
)

// Read collects every .psy file in directory as its own Source,
// preserving filenames for diagnostics.
func Read(directory string) ([]Source, error) {
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
