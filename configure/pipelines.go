package configure

import "errors"

var ErrNotImplemented = errors.New("configure package removed; use parse package for YAML parsing")

// The configure package previously provided HCL parsing helpers and pipeline models.
// The codebase was migrated to use the parse package (YAML). These stubs exist only
// to make builds that still import configure compile until those callsites are updated.

// LoadPipelines is a compatibility stub.
func LoadPipelines(filename string, literal []byte, _ interface{}) (map[string]interface{}, error) {
	return nil, ErrNotImplemented
}
