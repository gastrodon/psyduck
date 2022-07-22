package main

import (
	"fmt"

	"github.com/gastrodon/psyduck/config"
)

const yaml = `
pipelines:
    ifunny-data:
        producer:
            kind: ifunny-features
        consumer:
            kind: psyduck-trash
`

func main() {
	configured, err := config.Load([]byte(yaml))
	if err != nil {
		panic(err)
	}

	fmt.Printf("%#v\n", configured)
	fmt.Printf("%#v\n", configured.Pipelines["ifunny-data"])
}
