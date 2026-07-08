package parse

import (
	"fmt"
	"os"
	"path/filepath"
)

// Source is a chunk of configuration with enough identity to attribute
// errors back to its origin. It intentionally does not commit to being a
// file: a producer that generates config dynamically yields the same type.
//
// When Source comes from a real file (via SourceFromFile / a Loader),
// Name is a filesystem path and import{} attributes inside its Content
// resolve relative to filepath.Dir(Name).
type Source struct {
	Name    string // a filesystem path, or a synthetic label ("remote://abc123", "stdin", ...)
	Content []byte
}

// Loader reads the source found at path. Parsers call it on demand as
// import{} blocks are discovered, rather than requiring every reachable
// file to be read upfront. Keeping this as an injected function (instead
// of parse/hcl reading files directly) preserves Source's format-agnostic
// design and keeps import resolution unit-testable against in-memory
// fixtures.
type Loader func(path string) (Source, error)

// SourceFromFile reads a single .psy file as a Source, keyed by its path.
// Its signature matches Loader, so it can be passed anywhere a Loader is
// expected — the real, filesystem-backed one, as opposed to a fixture-based
// Loader a test might use instead.
func SourceFromFile(path string) (Source, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Source{}, fmt.Errorf("failed reading %s: %w", path, err)
	}
	return Source{Name: path, Content: content}, nil
}

// ResolveImportPath resolves an import{} path attribute (importPath)
// relative to the file that declared it (fromFile), and normalizes the
// result. Parsers use this so the same logical file always produces the
// same path string regardless of which importer reached it.
func ResolveImportPath(fromFile, importPath string) string {
	if filepath.IsAbs(importPath) {
		return filepath.Clean(importPath)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(fromFile), importPath))
}
