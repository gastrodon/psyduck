package plugins

import (
	"testing"

	"github.com/gastrodon/psyduck/parse"
)

func TestPluginKind(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   int
	}{
		{"empty", "", pluginUnknown},
		{"git ssh", "git@github.com:foo/bar.git", pluginRemote},
		{"https", "https://github.com/foo/bar.git", pluginRemote},
		{"https no .git suffix", "https://github.com/user/repo", pluginRemote},
		{"relative dot-slash", "./relative/path", pluginLocal},
		{"relative bare", "relative/path", pluginLocal},
		{"absolute", "/abs/path/to/plugin.so", pluginLocal},
		{"tilde-home", "~/plugins/myplugin", pluginLocal},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pluginKind(parse.Plugin{Source: c.source}); got != c.want {
				t.Errorf("pluginKind(%q) = %d, want %d", c.source, got, c.want)
			}
		})
	}
}
