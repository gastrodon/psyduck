package configure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type parseableFile struct {
	format   string
	name     string
	filepath string
	content  *string
}

func newParseFile(fp string) *parseableFile {
	return &parseableFile{
		format:   strings.TrimPrefix(filepath.Ext(fp), "."),
		name:     filepath.Base(fp),
		filepath: fp,
	}
}

func (f *parseableFile) readCached() (string, error) {
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

func (f *parseableFile) Name() string {
	return f.name
}

func (f *parseableFile) Parse() (*Config, error) {
	cont, err := f.readCached()
	if err != nil {
		return nil, err
	}

	cfg := new(Config)
	err = parse(f.format, cont, cfg)
	return cfg, err
}
