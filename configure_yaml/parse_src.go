package configure_yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ParseSRC interface {
	Format() string
	Name() string
	Content() (string, error)
}

type fileSRC struct {
	format   string
	name     string
	filepath string
	content  *string
}

func newFileSRC(fp string) *fileSRC {
	return &fileSRC{
		format:   strings.TrimPrefix(filepath.Ext(fp), "."),
		name:     filepath.Base(fp),
		filepath: fp,
	}
}

func (f *fileSRC) Format() string {
	return f.format
}

func (f *fileSRC) Name() string {
	return f.name
}

func (f *fileSRC) Content() (string, error) {
	if f.content == nil {
		data, err := os.ReadFile(f.filepath)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %v", f.Name(), err)
		}
		content := string(data)
		f.content = &content
	}

	return *f.content, nil
}
