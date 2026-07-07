package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/parse/hcl"
	"github.com/gastrodon/psyduck/stdlib"
)

type tier int

const (
	tierRun   tier = iota // parse + build + run; diff output against expect
	tierBuild             // parse + build only
	tierParse             // parse only
)

// fixture holds the test data for one example pipeline. The .psy sources
// live under examples/*.psy — one workspace, one file per example plus
// shared.psy for the consumers reused across them.
type fixture struct {
	tier   tier
	input  string // written to a temp file and injected as PSYDUCK_IN; empty = no input
	expect string // expected output, trailing newlines stripped; empty = not checked
}

// examples is keyed by pipeline name (matches the `pipeline "<name>"` block
// in examples/<name>.psy — except meta-socket.psy, which defines two).
var examples = map[string]fixture{
	"dev": {
		tier:   tierRun,
		expect: "n=1\nn=2\nn=3",
	},
	"encoding": {
		tier:   tierRun,
		expect: "aGVsbG8=\nd29ybGQ=",
	},
	"file-read": {
		tier:   tierRun,
		input:  "alpha\nbeta\ngamma",
		expect: "ALPHA\nBETA\nGAMMA",
	},
	"flow": {
		tier:   tierRun,
		expect: "0\n1\n2",
	},
	"jq": {
		tier:   tierRun,
		expect: "1",
	},
	"keyed": {
		tier:   tierRun,
		expect: "{\"id\":1}\n{\"id\":2}",
	},
	"render": {
		tier:   tierRun,
		expect: "hello ann",
	},
	"reshape": {
		tier:   tierRun,
		expect: "{\"name\":\"ann\",\"source\":\"e2e\"}\n{\"name\":\"bob\",\"source\":\"e2e\"}",
	},
	"select": {
		tier:   tierRun,
		expect: "ann\nbob",
	},
	"slicing": {
		tier:   tierRun,
		expect: "[\"ab\",\"cd\",\"ef\"]",
	},
	"text": {
		tier:   tierRun,
		expect: "HI_THERE",
	},
	"http-request": {tier: tierBuild},
	"http-listen":  {tier: tierBuild},
	"config-gen":   {tier: tierParse},
	"scrape":       {tier: tierParse},
}

// TestExamples runs each pipeline registered in the examples map as a subtest.
// Every subtest parses the whole examples/ workspace (so shared.psy and the
// per-example .psy files link up), then builds/runs only the target pipeline.
func TestExamples(t *testing.T) {
	for name, fix := range examples {
		fix := fix
		t.Run(name, func(t *testing.T) {
			runExample(t, name, fix)
		})
	}
}

func runExample(t *testing.T, name string, fix fixture) {
	t.Helper()
	plugins := []sdk.Plugin{stdlib.Plugin()}

	outPath := filepath.Join(t.TempDir(), "out")
	t.Setenv("PSYDUCK_OUT", outPath)

	if fix.input != "" {
		inPath := filepath.Join(t.TempDir(), "in")
		if err := os.WriteFile(inPath, []byte(fix.input), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PSYDUCK_IN", inPath)
	}

	sources, err := parse.SourceFromDir("examples")
	if err != nil {
		t.Fatalf("read examples/: %v", err)
	}
	pipelines, err := hcl.NewParserHCL().Parse(sources, plugins)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pipe, ok := pipelines[name]
	if !ok {
		t.Fatalf("pipeline %q not defined in examples/", name)
	}

	if fix.tier == tierParse {
		return
	}

	built, err := core.BuildPipeline(pipe, plugins)
	if err != nil {
		t.Fatalf("build %q: %v", name, err)
	}

	if fix.tier == tierBuild {
		return
	}

	if err := core.RunPipeline(built); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if normalize(got) != normalize([]byte(fix.expect)) {
		t.Errorf("output mismatch\n got: %q\nwant: %q", normalize(got), normalize([]byte(fix.expect)))
	}
}

// TestAssertFails proves the self-verifying path fails loudly: a false assert
// predicate must error the pipeline (with ExitOnError set).
func TestAssertFails(t *testing.T) {
	src := `
produce "generate" "src" { values = ["{\"ok\":false}"] }
transform "assert" "a" { expression = ".ok" }
consume "trash" "sink" {}
pipeline "check" {
  produce   = [produce.generate.src]
  transform = [transform.assert.a]
  consume   = [consume.trash.sink]
}`
	plugins := []sdk.Plugin{stdlib.Plugin()}
	pipelines, err := hcl.NewParserHCL().Parse(
		[]parse.Source{{Name: "assert.psy", Content: []byte(src)}}, plugins)
	if err != nil {
		t.Fatal(err)
	}
	bp, err := core.BuildPipeline(pipelines["check"], plugins)
	if err != nil {
		t.Fatal(err)
	}
	bp.ExitOnError = true
	if err := core.RunPipeline(bp); err == nil {
		t.Error("expected a false assert to error the pipeline")
	}
}

func normalize(b []byte) string { return strings.TrimRight(string(b), "\n") }
