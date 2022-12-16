module github.com/gastrodon/psyduck

go 1.18

require (
	github.com/hashicorp/hcl/v2 v2.15.0
	github.com/stretchr/testify v1.2.2
	github.com/urfave/cli/v2 v2.23.7
	github.com/zclconf/go-cty v1.12.1
)

require (
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/apparentlymart/go-textseg/v13 v13.0.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	golang.org/x/text v0.5.0 // indirect
)

replace github.com/zclconf/go-cty => ./replace/go-cty
