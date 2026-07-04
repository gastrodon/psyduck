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

// TestExamples runs every example under examples/ as a subtest. Each example is
// a directory with a main.psy; the verification tier is chosen by convention:
//
//   - expect.txt present  → parse, build, run, and diff the output against it.
//   - parse-only marker    → parse only (for produce-from / long-lived servers
//     whose build/run can't complete hermetically).
//   - otherwise            → parse + build only (validates specs & references
//     for network transports that shouldn't actually run).
//
// Runnable examples write their result to a file at env.PSYDUCK_OUT (injected
// per-example) and, where they read input, from env.PSYDUCK_IN.
func TestExamples(t *testing.T) {
	dirs, err := filepath.Glob("examples/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no examples found under examples/")
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			runExample(t, dir)
		})
	}
}

func runExample(t *testing.T, dir string) {
	t.Helper()
	plugins := []sdk.Plugin{stdlib.Plugin()}

	content, err := os.ReadFile(filepath.Join(dir, "main.psy"))
	if err != nil {
		t.Fatalf("read main.psy: %v", err)
	}

	// Inject I/O paths the example references via env.*. Keep the output path
	// in a local — env is only for the parser to read; we read the file back
	// from outPath so nothing can repoint PSYDUCK_OUT out from under us.
	outPath := filepath.Join(t.TempDir(), "out")
	t.Setenv("PSYDUCK_OUT", outPath)
	if in := filepath.Join(dir, "input.txt"); fileExists(in) {
		abs, err := filepath.Abs(in)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("PSYDUCK_IN", abs)
	}

	sources := []parse.Source{{Name: filepath.Base(dir) + "/main.psy", Content: content}}
	pipelines, err := hcl.NewParserHCL().Parse(sources, plugins)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pipelines) == 0 {
		t.Fatal("no pipelines defined")
	}

	// parse-only tier
	if fileExists(filepath.Join(dir, "parse-only")) {
		return
	}

	// build every pipeline
	built := make([]*core.Pipeline, 0, len(pipelines))
	for name, p := range pipelines {
		bp, err := core.BuildPipeline(p, plugins)
		if err != nil {
			t.Fatalf("build %q: %v", name, err)
		}
		built = append(built, bp)
	}

	// build-only tier
	expectPath := filepath.Join(dir, "expect.txt")
	if !fileExists(expectPath) {
		return
	}

	// run tier: run every pipeline (runnable examples are single-pipeline)
	for _, bp := range built {
		if err := core.RunPipeline(bp); err != nil {
			t.Fatalf("run: %v", err)
		}
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want, err := os.ReadFile(expectPath)
	if err != nil {
		t.Fatalf("read expect.txt: %v", err)
	}
	if normalize(got) != normalize(want) {
		t.Errorf("output mismatch\n got: %q\nwant: %q", normalize(got), normalize(want))
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

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
