package configure_yaml

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

// ParseDir reads all .yaml or .yml files in the directory and parses them into a Config.
// It merges the configurations from all files.
func ParseDir(directory string) (*Config, error) {
	cfg := &Config{}
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || (!strings.HasSuffix(info.Name(), ".yaml") && !strings.HasSuffix(info.Name(), ".yml")) {
			return nil
		}
		parsedCfg, err := newParseFile(path).Parse()
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		cfg.Pipelines = append(cfg.Pipelines, parsedCfg.Pipelines...)
		cfg.Plugins = append(cfg.Plugins, parsedCfg.Plugins...)
		return nil
	})

	return cfg, err
}
