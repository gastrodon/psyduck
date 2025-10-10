package configure

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"time"

	"github.com/psyduck-etl/sdk"
)

// PluginYAML represents a plugin definition in YAML format.
type PluginYAML struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Source    string `yaml:"source"`
	Tag       string `yaml:"tag,omitempty"`
	diskCache *string
}

func gitClone(source, cloneDir string) error {
	cmd := exec.Command("git", "clone", source, cloneDir)
	return cmd.Run()
}

func goBuildPlugin(cloneDir string) (string, error) {
	soPath := filepath.Join(cloneDir, fmt.Sprintf("plugin_%d.so", time.Now().UnixNano()))
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", soPath, cloneDir)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return soPath, nil
}
func (p *PluginYAML) Fetch() error {
	switch p.Kind {
	case "disk":
		p.diskCache = &p.Source
		return nil
	case "git":
		// Create a unique temporary directory
		cloneDir, err := os.MkdirTemp("", "plugin_git_clone_*")
		if err != nil {
			return fmt.Errorf("failed to create temporary directory: %w", err)
		}
		defer os.RemoveAll(cloneDir) // Clean up after use

		// Clone the repository
		err = gitClone(p.Source, cloneDir)
		if err != nil {
			return fmt.Errorf("failed to clone git repository: %w", err)
		}

		// Build the plugin
		soPath, err := goBuildPlugin(cloneDir)
		if err != nil {
			return fmt.Errorf("failed to build plugin: %w", err)
		}

		p.diskCache = &soPath
		return nil
	default:
		return fmt.Errorf("idk what that kind is")
	}
}

func (p *PluginYAML) Load() (*sdk.Plugin, error) {
	if p.diskCache == nil {
		if err := p.Fetch(); err != nil {
			return nil, err
		}
	}

	pluginLib, err := plugin.Open(*p.diskCache)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin %s: %s", *p.diskCache, err)
	}

	makePluginSym, err := pluginLib.Lookup("Plugin")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup Plugin in %s: %s", *p.diskCache, err)
	}

	makePlugin, ok := makePluginSym.(func() *sdk.Plugin)
	if !ok {
		return nil, fmt.Errorf("plugin symbol in %s is not func() *sdk.Plugin", *p.diskCache)
	}

	return makePlugin(), nil
}
