package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
// live under examples/*.psy — each file is its own entry point; cross-file
// reuse (e.g. shared.psy's consumers) goes through explicit import{} blocks
// rather than directory-wide sharing.
type fixture struct {
	file   string // path under examples/; defaults to "<pipeline name>.psy"
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
	"fan-in": {
		tier:   tierRun,
		expect: "hello\n0\n1",
	},
	"http-request": {tier: tierBuild},
	"http-listen":  {tier: tierBuild},
	"config-gen":   {file: "meta-socket.psy", tier: tierParse},
	"scrape":       {file: "meta-socket.psy", tier: tierParse},
}

// TestExamples runs each pipeline registered in the examples map as a subtest.
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

	file := fix.file
	if file == "" {
		file = name + ".psy"
	}
	entry := filepath.Join("examples", file)

	pipelines, err := hcl.NewParserHCL().Parse(t.Context(), entry, parse.FileLoader, plugins)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pipe, ok := pipelines[name]
	if !ok {
		t.Fatalf("pipeline %q not defined in %s", name, entry)
	}

	if fix.tier == tierParse {
		return
	}

	built, err := core.BuildPipeline(t.Context(), pipe, plugins)
	if err != nil {
		t.Fatalf("build %q: %v", name, err)
	}

	if fix.tier == tierBuild {
		return
	}

	if err := core.RunPipeline(t.Context(), built); err != nil {
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
	load := func(path string) (parse.Source, error) {
		return parse.Source{Name: path, Content: []byte(src)}, nil
	}
	pipelines, err := hcl.NewParserHCL().Parse(t.Context(), "assert.psy", load, plugins)
	if err != nil {
		t.Fatal(err)
	}
	bp, err := core.BuildPipeline(t.Context(), pipelines["check"], plugins)
	if err != nil {
		t.Fatal(err)
	}
	bp.ExitOnError = true
	if err := core.RunPipeline(t.Context(), bp); err == nil {
		t.Error("expected a false assert to error the pipeline")
	}
}

// TestProduceFromSocket runs the two produce-from-socket.psy pipelines at the
// same time: "emit" renders two constant producers into produce descriptors
// and writes them to a unix socket; "run" listens on that socket and executes
// each received producer via produce-from. It is the full socket -> meta-
// producer round trip end to end, where meta-socket.psy only parses.
//
// "run" binds the socket, so it starts first; once bound, "emit" dials and
// writes. "run"'s listen producer never exhausts on its own — its own
// stop-after = 2 meta (produce-only; produce-from seeds honor it the same as
// any other producer) ends it after both descriptors have been run — so a
// stuck run shows up as the timeout below, and the output order (alpha then
// beta) proves framing and produce-parallel=1 preserve order across the
// socket.
func TestProduceFromSocket(t *testing.T) {
	const sockPath = "/tmp/psyduck-e2e.sock"
	_ = os.Remove(sockPath) // clear a stale socket from a crashed run
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	plugins := []sdk.Plugin{stdlib.Plugin()}

	outPath := filepath.Join(t.TempDir(), "out")
	t.Setenv("PSYDUCK_OUT", outPath)

	entry := filepath.Join("examples", "produce-from-socket.psy")
	pipelines, err := hcl.NewParserHCL().Parse(t.Context(), entry, parse.FileLoader, plugins)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	emit, err := core.BuildPipeline(t.Context(), pipelines["emit"], plugins)
	if err != nil {
		t.Fatalf("build emit: %v", err)
	}
	run, err := core.BuildPipeline(t.Context(), pipelines["run"], plugins)
	if err != nil {
		t.Fatalf("build run: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// run binds and listens; start it first so emit has something to dial.
	runErr := make(chan error, 1)
	go func() { runErr <- core.RunPipeline(ctx, run) }()

	waitForFile(t, sockPath, 5*time.Second)

	// emit connects, writes both descriptors, and returns once done.
	if err := core.RunPipeline(ctx, emit); err != nil {
		t.Fatalf("run emit: %v", err)
	}

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run pipeline did not finish after emit completed")
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if want := "alpha\nbeta"; normalize(got) != want {
		t.Errorf("output mismatch\n got: %q\nwant: %q", normalize(got), want)
	}
}

// waitForFile blocks until path exists or the deadline passes.
func waitForFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s not bound within %s", path, within)
}

func normalize(b []byte) string { return strings.TrimRight(string(b), "\n") }
