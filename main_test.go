package main

import (
	"strings"
	"testing"

	"github.com/gastrodon/psyduck/parse"
)

func TestSelectPipelines(t *testing.T) {
	pipelines := map[string]parse.Pipeline{
		"gate":     {},
		"snapshot": {},
		"tags":     {},
	}

	t.Run("no names selects everything", func(t *testing.T) {
		got, err := selectPipelines(pipelines, nil)
		if err != nil {
			t.Fatalf("selectPipelines: %v", err)
		}
		if len(got) != len(pipelines) {
			t.Fatalf("want %d pipelines, got %d", len(pipelines), len(got))
		}
	})

	t.Run("names filter", func(t *testing.T) {
		got, err := selectPipelines(pipelines, []string{"gate", "tags"})
		if err != nil {
			t.Fatalf("selectPipelines: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 pipelines, got %d", len(got))
		}
		for _, name := range []string{"gate", "tags"} {
			if _, ok := got[name]; !ok {
				t.Errorf("missing %q", name)
			}
		}
	})

	t.Run("pipeline. prefix is accepted", func(t *testing.T) {
		got, err := selectPipelines(pipelines, []string{"pipeline.snapshot"})
		if err != nil {
			t.Fatalf("selectPipelines: %v", err)
		}
		if _, ok := got["snapshot"]; !ok || len(got) != 1 {
			t.Fatalf("want just snapshot, got %v", got)
		}
	})

	t.Run("repeats collapse", func(t *testing.T) {
		got, err := selectPipelines(pipelines, []string{"gate", "gate", "pipeline.gate"})
		if err != nil {
			t.Fatalf("selectPipelines: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 pipeline, got %d", len(got))
		}
	})

	t.Run("unknown name errors, listing declared", func(t *testing.T) {
		_, err := selectPipelines(pipelines, []string{"nope"})
		if err == nil {
			t.Fatal("want error for unknown pipeline")
		}
		for _, want := range []string{`"nope"`, "gate", "snapshot", "tags"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err.Error(), want)
			}
		}
	})
}
