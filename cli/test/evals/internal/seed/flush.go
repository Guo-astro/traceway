package seed

import (
	"fmt"
	"sort"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	pathTraces  = "/api/otel/v1/traces"
	pathMetrics = "/api/otel/v1/metrics"
	pathLogs    = "/api/otel/v1/logs"
	pathReport  = "/api/report"
	protoType   = "application/x-protobuf"
	// spansPerRequest keeps each OTLP body far below the 10MB ingest cap.
	spansPerRequest = 1500
)

// Flush generates every request, resolves lazy exposes, enforces the span
// budget and sends everything: traces, logs, metrics, native SDK frames.
func (s *Scenario) Flush() error {
	if s.flushed {
		return fmt.Errorf("scenario flushed twice")
	}
	s.flushed = true
	started := time.Now()

	for _, svc := range s.services {
		applyDefaults(svc)
	}
	var all []*Request
	for _, svc := range s.services {
		for _, e := range append(append([]*Endpoint{}, svc.endpoints...), svc.tasks...) {
			if len(e.shapes) == 0 {
				continue // only reached through Call
			}
			all = append(all, e.Requests()...)
		}
	}
	keys := make([]string, 0, len(s.lazy))
	for k := range s.lazy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s.exposed[k] = s.lazy[k]()
	}
	s.enforceBudget(all)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Start.Before(all[j].Start) })

	batches := map[*Project]*spanBatch{}
	logs := map[*Service][]logOut{}
	for _, r := range all {
		s.stats.Requests++
		if r.Detail {
			s.stats.DetailedReqs++
		}
		s.emit(r, batches, logs)
		for p, b := range batches {
			if b.count >= spansPerRequest {
				if err := s.sendTraces(p, b); err != nil {
					return err
				}
				delete(batches, p)
			}
		}
	}
	projects := make([]*Project, 0, len(batches))
	for p := range batches {
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	for _, p := range projects {
		if err := s.sendTraces(p, batches[p]); err != nil {
			return err
		}
	}

	for _, svc := range s.services {
		outs := logs[svc]
		for _, l := range svc.logs {
			outs = append(outs, logOut{rec: l, at: l.abs, host: l.host, version: svc.VersionAt(l.abs), noTrace: true})
		}
		if len(outs) == 0 {
			continue
		}
		sort.SliceStable(outs, func(i, j int) bool { return outs[i].at.Before(outs[j].at) })
		for i := 0; i < len(outs); i += 2000 {
			j := min(i+2000, len(outs))
			body, err := proto.Marshal(logsRequest(svc, outs[i:j]))
			if err != nil {
				return err
			}
			if _, err := s.Sink.Ingest(svc.p.token, pathLogs, protoType, body, true); err != nil {
				return fmt.Errorf("logs for %s: %w", svc.Name, err)
			}
			s.stats.Logs += j - i
		}
	}

	for _, svc := range s.services {
		for _, req := range s.metricRequests(svc) {
			body, err := proto.Marshal(req)
			if err != nil {
				return err
			}
			if _, err := s.Sink.Ingest(svc.p.token, pathMetrics, protoType, body, true); err != nil {
				return fmt.Errorf("metrics for %s: %w", svc.Name, err)
			}
		}
	}

	recordings := 0
	for _, p := range s.projects {
		for _, f := range p.native {
			recordings += len(f.recordings)
		}
		bodies, err := p.nativeBodies()
		if err != nil {
			return err
		}
		for _, body := range bodies {
			if _, err := s.Sink.Ingest(p.token, pathReport, "application/json", body, true); err != nil {
				return fmt.Errorf("native report for %s: %w", p.Name, err)
			}
			s.stats.NativeFrames++
		}
	}

	if recordings > 0 && !s.Sink.Dry() {
		// Session recordings are uploaded by a worker pool and inserted in
		// batches every ~2s; wait so occurrences show their actions.
		time.Sleep(3 * time.Second)
	}
	for _, f := range s.afterFlush {
		if err := f(); err != nil {
			return err
		}
	}
	s.stats.IngestElapsed = time.Since(started)
	return nil
}

func (s *Scenario) sendTraces(p *Project, b *spanBatch) error {
	body, err := proto.Marshal(b.request())
	if err != nil {
		return err
	}
	resp, err := s.Sink.Ingest(p.token, pathTraces, protoType, body, true)
	if err != nil {
		return fmt.Errorf("traces for %s: %w", p.Name, err)
	}
	if len(resp) > 0 {
		var out coltracepb.ExportTraceServiceResponse
		if proto.Unmarshal(resp, &out) == nil && out.GetPartialSuccess().GetRejectedSpans() > 0 {
			return fmt.Errorf("traces for %s: backend rejected %d spans: %s", p.Name, out.GetPartialSuccess().GetRejectedSpans(), out.GetPartialSuccess().GetErrorMessage())
		}
	}
	return nil
}

func spanCount(sp *Span) int {
	n := 1
	for _, c := range sp.Children {
		n += spanCount(c)
	}
	return n
}

func serverSpans(sp *Span) int {
	n := 0
	if sp.Kind == KindServer {
		n = 1
	}
	for _, c := range sp.Children {
		n += serverSpans(c)
	}
	return n
}

// enforceBudget keeps the detailed span volume under SpanBudget by dropping
// sampled (not forced) detail, oldest first.
func (s *Scenario) enforceBudget(all []*Request) {
	total := 0
	for _, r := range all {
		total += s.sentSpans(r)
	}
	if total <= s.SpanBudget {
		return
	}
	sampled := make([]*Request, 0)
	for _, r := range all {
		if r.Detail && !r.forced {
			sampled = append(sampled, r)
		}
	}
	sort.SliceStable(sampled, func(i, j int) bool { return sampled[i].Start.Before(sampled[j].Start) })
	for _, r := range sampled {
		if total <= s.SpanBudget {
			return
		}
		before := s.sentSpans(r)
		r.Detail = false
		total -= before - s.sentSpans(r)
	}
}

func (s *Scenario) sentSpans(r *Request) int {
	if r.Detail {
		return spanCount(r.Root)
	}
	return 1 + serverSpans(r.Root) - boolInt(r.Root.Kind == KindServer)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
