package main

import (
	"errors"
	"flag"
	"fmt"

	std "github.com/gastrodon/psyduck-std"
	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/sdk"
)

func library() *core.Library {
	made := core.NewLibrary()
	made.Load(std.IFunny())
	made.Load(std.Psyduck())
	made.Load(std.Scyther())

	return made
}

func main() {
	directory := flag.String("where", ".", "Directory with psyduck configs")
	pipelineName := flag.String("pipeline", "", "Pipelines to run")
	flag.Parse()

	if *pipelineName == "" {
		panic(errors.New("flag pipeline requires a value"))
	}

	if descriptors, context, err := configure.Directory(*directory); err != nil {
		panic(err)
	} else if descriptor, ok := descriptors[*pipelineName]; !ok {
		panic(fmt.Errorf("can't find pipeline %s", *pipelineName))
	} else {
		if pipeline, err := core.BuildPipeline(descriptor, context, library()); err != nil {
			panic(err)
		} else {
			signal := make(sdk.Signal)

			if err := core.RunPipeline(pipeline, signal); err != nil {
				panic(err)
			}
		}
	}
}
