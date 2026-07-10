package server

import (
	"net/http"
	"strconv"
	"strings"
)

// handleMetrics serves the Prometheus/OpenMetrics text exposition at
// GET /metrics. This is the Grafana-facing surface: point a Prometheus
// scrape at it and the pipeline counters become dashboards. It carries the
// same numbers as the JSON stats endpoints, in the format a metrics stack
// expects, because the two audiences differ — a UI wants JSON it can shape,
// a scraper wants labeled counters it can range over.
//
// The exposition is written by hand rather than via a client library so the
// module stays dependency-free; the format is small and stable.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	inst := s.sup.Instance()
	pipelines := s.sup.List()

	var b strings.Builder

	writeMetricHeader(&b, "psyduck_build_info", "gauge", "Instance build/version info; value is always 1.")
	b.WriteString("psyduck_build_info{version=" + quote(inst.Version) + ",instance=" + quote(inst.ID) + "} 1\n")

	writeMetricHeader(&b, "psyduck_instance_uptime_seconds", "gauge", "Seconds since this instance started serving.")
	b.WriteString("psyduck_instance_uptime_seconds " + strconv.FormatFloat(inst.UptimeSeconds, 'f', 3, 64) + "\n")

	writeMetricHeader(&b, "psyduck_pipelines", "gauge", "Pipelines known to this instance, by status.")
	c := inst.Pipelines
	for _, kv := range []struct {
		status string
		n      int
	}{
		{"running", c.Running}, {"pending", c.Pending}, {"succeeded", c.Succeeded},
		{"failed", c.Failed}, {"canceled", c.Canceled},
	} {
		b.WriteString("psyduck_pipelines{status=" + quote(kv.status) + "} " + strconv.Itoa(kv.n) + "\n")
	}

	// Per-pipeline counters. A UI diffs these snapshots for rates; Grafana
	// does the same with rate()/increase(). in_flight is a gauge (lag).
	counters := []struct {
		name, help string
		get        func(Stats) uint64
	}{
		{"psyduck_pipeline_messages_produced_total", "Messages emitted by a pipeline's producers.", func(s Stats) uint64 { return s.Produced }},
		{"psyduck_pipeline_messages_transformed_total", "Messages that passed the transform stack.", func(s Stats) uint64 { return s.Transformed }},
		{"psyduck_pipeline_messages_filtered_total", "Messages dropped by a transformer.", func(s Stats) uint64 { return s.Filtered }},
		{"psyduck_pipeline_messages_delivered_total", "Messages delivered to consumers.", func(s Stats) uint64 { return s.Delivered }},
		{"psyduck_pipeline_errors_total", "Errors reported by any stage.", func(s Stats) uint64 { return s.Errors }},
	}
	for _, m := range counters {
		writeMetricHeader(&b, m.name, "counter", m.help)
		for _, p := range pipelines {
			b.WriteString(m.name + pipelineLabels(p) + " " + strconv.FormatUint(m.get(p.Stats), 10) + "\n")
		}
	}

	writeMetricHeader(&b, "psyduck_pipeline_in_flight", "gauge", "Messages produced but not yet delivered/filtered/errored (coarse lag).")
	for _, p := range pipelines {
		b.WriteString("psyduck_pipeline_in_flight" + pipelineLabels(p) + " " + strconv.FormatInt(p.Stats.InFlight, 10) + "\n")
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// pipelineLabels renders the common {pipeline,name} label set for a series.
func pipelineLabels(p PipelineInfo) string {
	return "{pipeline=" + quote(p.ID) + ",name=" + quote(p.Name) + "}"
}

func writeMetricHeader(b *strings.Builder, name, typ, help string) {
	b.WriteString("# HELP " + name + " " + help + "\n")
	b.WriteString("# TYPE " + name + " " + typ + "\n")
}

// quote renders a Prometheus label value: double-quoted with backslash,
// double-quote, and newline escaped, per the exposition format.
func quote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(v) + `"`
}
