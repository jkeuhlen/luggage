package ui

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"luggage/internal/metrics"
)

func TestSampleCharts(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)

	cases := []struct {
		name   string
		params TimeReportParams
		series [][]float64
	}{
		{
			name: "daily_dense_active",
			params: TimeReportParams{
				Command:         "gs",
				View:            "typed",
				Granularity:     metrics.Daily,
				Days:            30,
				Window:          "active",
				IncludeSessions: false,
				SuccessOnly:     false,
			},
			series: [][]float64{
				{18, 22, 20},
				{20, 24, 23},
				{26, 30, 34},
				{31, 33, 36},
				{29, 32, 31},
				{40, 45, 52},
				{47, 50, 58},
				{44, 46, 49},
			},
		},
		{
			name: "daily_sparse_hidden",
			params: TimeReportParams{
				Command:         "gs",
				View:            "typed",
				Granularity:     metrics.Daily,
				Days:            30,
				Window:          "active",
				IncludeSessions: false,
				SuccessOnly:     false,
			},
			series: [][]float64{
				nil,
				{18, 19},
				nil,
				nil,
				{25, 26},
				nil,
			},
		},
		{
			name: "daily_dense_full_window",
			params: TimeReportParams{
				Command:         "gs",
				View:            "typed",
				Granularity:     metrics.Daily,
				Days:            90,
				Window:          "full",
				IncludeSessions: false,
				SuccessOnly:     false,
			},
			series: [][]float64{
				nil,
				{15, 18, 20},
				nil,
				{22, 24, 27},
				nil,
				nil,
				{35, 36, 39},
				nil,
				{43, 45, 50},
				nil,
				{40, 41, 43},
				nil,
			},
		},
	}

	update := os.Getenv("UPDATE_GOLDEN") == "1"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buckets := makeBuckets(start, tc.series)
			got := RenderTimeReport(tc.params, buckets)

			goldenPath := filepath.Join("testdata", tc.name+".golden.txt")
			if update {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}

			wantBytes, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v", goldenPath, err)
			}
			want := string(wantBytes)

			if got != want {
				t.Fatalf("report mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tc.name, got, want)
			}

			t.Logf("\n%s", got)

			if tc.name == "daily_sparse_hidden" && !strings.Contains(got, "Trend chart hidden") {
				t.Fatalf("expected sparse sample to hide chart")
			}
			if tc.name != "daily_sparse_hidden" && !strings.Contains(got, "median |") {
				t.Fatalf("expected dense sample to include sparkline chart")
			}
		})
	}
}

func makeBuckets(start time.Time, series [][]float64) []metrics.Bucket {
	out := make([]metrics.Bucket, 0, len(series))
	for i, vals := range series {
		ts := start.AddDate(0, 0, i)
		b := metrics.Bucket{Start: ts}
		if len(vals) == 0 {
			out = append(out, b)
			continue
		}
		clean := append([]float64(nil), vals...)
		b.Count = len(clean)
		b.DurationsMs = clean
		b.MeanMs = mean(clean)
		b.MedianMs = testPercentile(clean, 50)
		b.P95Ms = testPercentile(clean, 95)
		out = append(out, b)
	}
	return out
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	total := 0.0
	for _, v := range vals {
		total += v
	}
	return total / float64(len(vals))
}

func testPercentile(vals []float64, p int) float64 {
	if len(vals) == 0 {
		return 0
	}
	clone := append([]float64(nil), vals...)
	sort.Float64s(clone)
	if p <= 0 {
		return clone[0]
	}
	if p >= 100 {
		return clone[len(clone)-1]
	}
	rank := (float64(p) / 100.0) * float64(len(clone)-1)
	low := int(math.Floor(rank))
	high := int(math.Ceil(rank))
	if low == high {
		return clone[low]
	}
	weight := rank - float64(low)
	return clone[low]*(1-weight) + clone[high]*weight
}
