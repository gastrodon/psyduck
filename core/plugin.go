package core

import (
	"plugin"

	"github.com/psyduck-std/sdk"
)

func LoadPlugin(path string) (*sdk.Plugin, error) {
	loaded, err := plugin.Open(path)
	if err != nil {
		return nil, err
	}

	callable, err := loaded.Lookup("Plugin")
	if err != nil {
		return nil, err
	}

	return callable.(func() *sdk.Plugin)(), nil
}
