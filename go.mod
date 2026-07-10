module github.com/gastrodon/psyduck

go 1.24.0

toolchain go1.24.13

require (
	github.com/hashicorp/hcl/v2 v2.20.1
	github.com/itchyny/gojq v0.12.19
	github.com/psyduck-etl/sdk v0.5.3-0.20260710202941-89fdca3cd8a6
	github.com/sirupsen/logrus v1.9.3
	github.com/urfave/cli/v2 v2.27.1
	github.com/zclconf/go-cty v1.14.4
)

require (
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.4 // indirect
	github.com/itchyny/timefmt-go v0.1.8 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/xrash/smetrics v0.0.0-20240312152122-5f08fbb34913 // indirect
	golang.org/x/mod v0.17.0 // indirect
	golang.org/x/sync v0.7.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/tools v0.20.0 // indirect
)

replace github.com/zclconf/go-cty => github.com/gastrodon/go-cty v1.14.4-1
