package parse

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
)

func drawDiags(d hcl.Diagnostics) string {
	buf := make([]string, len(d))
	for i, diag := range d {
		buf[i] = diag.Error()
	}

	return strings.Join(buf, "\n")
}
