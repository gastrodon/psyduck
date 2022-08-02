package config

import (
	"github.com/hashicorp/hcl/v2"
)

func makeValuesContext(values *Values) *hcl.EvalContext {
	return &hcl.EvalContext{
		Variables: values.Values,
	}
}
