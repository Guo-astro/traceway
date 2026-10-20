package seed

import (
	"testing"
	"time"
)

// TestHistogramDeltaWindows: each delta point starts where the previous one
// ended, across the chunks a series is rendered in, and counts the
// observations of exactly that window.
func TestHistogramDeltaWindows(t *testing.T) {
	m := &metricSeries{name: "h", kind: histogramKind, value: func(time.Time) float64 { return 5 }, count: func(time.Time) float64 { return 10 }}
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var times []time.Time
	for i := range 6 {
		times = append(times, base.Add(time.Duration(i)*MetricStep.Old))
	}
	var points []struct {
		start, end uint64
		count      uint64
	}
	for _, chunk := range [][]time.Time{times[:3], times[3:]} {
		for _, dp := range m.render(chunk, base).GetHistogram().DataPoints {
			points = append(points, struct {
				start, end uint64
				count      uint64
			}{dp.StartTimeUnixNano, dp.TimeUnixNano, dp.Count})
		}
	}
	for i, p := range points {
		if p.count != 100 {
			t.Errorf("point %d counts %d observations, want 100 (10/min over 10 min)", i, p.count)
		}
		if i > 0 && p.start != points[i-1].end {
			t.Errorf("point %d starts at %d, want the previous point's end %d", i, p.start, points[i-1].end)
		}
		if got := time.Duration(p.end - p.start); got != MetricStep.Old {
			t.Errorf("point %d covers %s, want %s", i, got, MetricStep.Old)
		}
	}
}
