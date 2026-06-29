package datasource

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

// parseValueBlocks decodes value {} blocks from an HCL body and returns
// the merged key-value entries. Duplicate keys across blocks are an error.
func parseValueBlocks(body hcl.Body) (map[string]cty.Value, error) {
	target := new(struct {
		hcl.Body `hcl:",remain"`
		Blocks   []struct {
			Entries map[string]cty.Value `hcl:",remain"`
		} `hcl:"value,block"`
	})

	if diags := gohcl.DecodeBody(body, nil, target); diags.HasErrors() {
		return nil, diags
	}

	values := make(map[string]cty.Value)
	for _, b := range target.Blocks {
		for k, v := range b.Entries {
			if _, exists := values[k]; exists {
				return nil, fmt.Errorf("duplicate value key: %s", k)
			}
			values[k] = v
		}
	}

	return values, nil
}

// Value parses HCL containing value { ... } blocks and returns a
// Datasource[cty.Value] backed by the merged key-value entries.
func Value(filename string, literal []byte) (Datasource[cty.Value], error) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	values, err := parseValueBlocks(file.Body)
	if err != nil {
		return nil, err
	}

	return &mapDatasource[cty.Value]{data: values}, nil
}
