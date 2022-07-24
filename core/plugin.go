package core

import (
	"plugin"

	"github.com/gastrodon/psyduck/model"
)

func LoadPlugin(path string) (*model.Plugin, error) {
	loaded, err := plugin.Open(path)
	if err != nil {
		return nil, err
	}

	callable, err := loaded.Lookup("Plugin")
	if err != nil {
		return nil, err
	}

	return callable.(func() *model.Plugin)(), nil
}
