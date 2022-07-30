package config

import (
	"strings"
)

func nameResource(resource *Resource) string {
	return strings.Join([]string{resource.Kind, resource.Name}, ".")
}

func makeResourcesLookup(resources []*Resource) map[string]*Resource {
	lookup := make(map[string]*Resource, len(resources))

	for _, each := range resources {
		lookup[nameResource(each)] = each
	}

	return lookup

}

func makeResources(raw *ResourcesRaw) *Resources {
	return &Resources{
		Producers:    makeResourcesLookup(raw.Producers),
		Consumers:    makeResourcesLookup(raw.Consumers),
		Transformers: makeResourcesLookup(raw.Transformers),
	}
}
