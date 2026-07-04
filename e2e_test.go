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

// TestEndToEndCodecPipeline runs a real pipeline through the HCL decoder, the
// builder, and the runtime — exercising the new codec transformers and the
// file consumer against the actual config machinery (map/list/required specs,
// defaults). It writes generated + reshaped JSON to a temp file and checks it.
func TestEndToEndCodecPipeline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.jsonl")
	src := `
produce "generate" "src" {
  values = [
    "{\"user\": {\"name\": \"ann\"}, \"drop_me\": 1}",
    "{\"user\": {\"name\": \"bob\"}, \"drop_me\": 2}",
  ]
}

transform "pick-map" "reshape" {
  fields = { "name" = ["user", "name"] }
}

transform "set" "tag" {
  values = { "source" = "e2e" }
}

consume "file" "out" {
  location = "` + out + `"
  sep      = "\n"
}

pipeline "shape" {
  produce   = [produce.generate.src]
  transform = [transform.pick-map.reshape, transform.set.tag]
  consume   = [consume.file.out]
}
`
	runSource(t, src, "shape")

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := strings.TrimSpace(string(data))
	want := `{"name":"ann","source":"e2e"}
{"name":"bob","source":"e2e"}`
	if got != want {
		t.Errorf("pipeline output:\n%s\nwant:\n%s", got, want)
	}
}

// TestEndToEndRequestConfig confirms the request resource — with map headers,
// a list of success codes, and defaults — decodes through the real HCL path.
func TestEndToEndRequestConfig(t *testing.T) {
	src := `
produce "request" "api" {
  url           = "http://example.invalid/items"
  headers       = { "Authorization" = "Bearer x" }
  query-params  = { "page" = "1" }
  success-codes = [200, 204]
  stop-after    = 1
}

consume "trash" "sink" {}

pipeline "fetch" {
  produce = [produce.request.api]
  consume = [consume.trash.sink]
}
`
	// Build only — we don't hit the network. A successful build proves the
	// spec (maps, int list, required url) decodes.
	if _, err := buildPipeline(t, src, "fetch"); err != nil {
		t.Fatalf("build request pipeline: %v", err)
	}
}

func buildPipeline(t *testing.T, src, name string) (*core.Pipeline, error) {
	t.Helper()
	sources := []parse.Source{{Name: name + ".psy", Content: []byte(src)}}
	pipelines, err := hcl.NewParserHCL().Parse(sources, []sdk.Plugin{stdlib.Plugin()})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, ok := pipelines[name]
	if !ok {
		t.Fatalf("pipeline %q not found", name)
	}
	return core.BuildPipeline(p, []sdk.Plugin{stdlib.Plugin()})
}

func runSource(t *testing.T, src, name string) {
	t.Helper()
	pipeline, err := buildPipeline(t, src, name)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := core.RunPipeline(pipeline); err != nil {
		t.Fatalf("run: %v", err)
	}
}
