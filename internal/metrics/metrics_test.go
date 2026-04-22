package metrics

import (
	"testing"
	"time"

	"luggage/internal/store"
)

func TestBuildBucketsDaily(t *testing.T) {
	loc := time.UTC
	rows := []store.RunRow{
		{StartedAt: time.Date(2026, 4, 20, 8, 0, 0, 0, loc), Duration: 100},
		{StartedAt: time.Date(2026, 4, 20, 10, 0, 0, 0, loc), Duration: 300},
		{StartedAt: time.Date(2026, 4, 21, 9, 0, 0, 0, loc), Duration: 200},
	}
	start := time.Date(2026, 4, 20, 0, 0, 0, 0, loc)
	end := time.Date(2026, 4, 21, 0, 0, 0, 0, loc)
	buckets := BuildBuckets(rows, start, end, Daily, loc)
	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(buckets))
	}
	if buckets[0].Count != 2 {
		t.Fatalf("expected bucket 0 count 2, got %d", buckets[0].Count)
	}
	if buckets[0].MeanMs != 200 {
		t.Fatalf("expected mean 200, got %v", buckets[0].MeanMs)
	}
	if buckets[1].MedianMs != 200 {
		t.Fatalf("expected median 200, got %v", buckets[1].MedianMs)
	}
}

func TestDetectAnomalies(t *testing.T) {
	loc := time.UTC
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, loc)
	buckets := make([]Bucket, 0, 20)
	for i := 0; i < 15; i++ {
		buckets = append(buckets, Bucket{Start: base.AddDate(0, 0, i), Count: 10, MedianMs: 100 + float64(i%3), P95Ms: 120})
	}
	buckets = append(buckets, Bucket{Start: base.AddDate(0, 0, 15), Count: 10, MedianMs: 300, P95Ms: 500})
	DetectAnomalies(buckets, 14, 2.5)
	if !buckets[15].Anomaly {
		t.Fatalf("expected anomaly at index 15")
	}
}

func TestSummary(t *testing.T) {
	buckets := []Bucket{{Count: 2, DurationsMs: []float64{100, 300}}, {Count: 1, DurationsMs: []float64{200}}}
	runs, meanMs, medianMs, p95Ms := Summary(buckets)
	if runs != 3 {
		t.Fatalf("expected 3 runs, got %d", runs)
	}
	if meanMs != 200 {
		t.Fatalf("expected mean 200, got %v", meanMs)
	}
	if medianMs != 200 {
		t.Fatalf("expected median 200, got %v", medianMs)
	}
	if p95Ms <= 200 {
		t.Fatalf("expected p95 > 200, got %v", p95Ms)
	}
}
