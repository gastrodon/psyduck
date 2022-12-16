package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/core"
)

func run() error {
	context := flag.String("context", ".", "execution directory")
	target := flag.String("target", "", "pipelines to target")
	flag.Parse()

	if *target == "" {
		return errors.New("-target is required")
	}

	descriptors, exprContext, err := configure.Directory(*context)
	if err != nil {
		return err
	}

	descriptor, ok := descriptors[*target]
	if !ok {
		return fmt.Errorf("can't find target %s", *target)
	}

	pipeline, err := core.BuildPipeline(descriptor, exprContext, core.NewLibrary())
	if err != nil {
		return err
	}

	return core.RunPipeline(pipeline)
}

var handles = map[string]func() error{
	"run": run,
}

func usage() {
	commands := make([]string, len(handles))
	index := 0
	for key := range handles {
		commands[index] = key
		index++
	}

	name := strings.Split(os.Args[0], string(os.PathSeparator))
	basename := name[len(name)-1]

	fmt.Printf("Usage %s <subcommand>\n\n", basename)
	fmt.Printf("Allowed subcommands are\n  %s\n\n", strings.Join(commands, "\n  "))
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	if handle, ok := handles[os.Args[1]]; !ok {
		usage()
		os.Exit(1)
	} else {
		if err := handle(); err != nil {
			panic(err)
		}
	}
}
