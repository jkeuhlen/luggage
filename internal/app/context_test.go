package app

import (
	"testing"
	"time"

	"luggage/internal/store"
)

func TestBuildContextRunsLocationModes(t *testing.T) {
	start := time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	rows := []store.RunRow{
		{
			StartedAt: start.Add(5 * time.Minute),
			Duration:  5000,
			Cwd:       "/work/repo/sub",
			GitRoot:   "/work/repo",
		},
		{
			StartedAt: start.Add(15 * time.Minute),
			Duration:  4000,
			Cwd:       "/tmp",
			GitRoot:   "",
		},
	}

	repoRuns := buildContextRuns(rows, "repo", start, end)
	if len(repoRuns) != 2 {
		t.Fatalf("expected 2 repo runs, got %d", len(repoRuns))
	}
	if repoRuns[0].Location != "/work/repo" {
		t.Fatalf("expected git root location, got %q", repoRuns[0].Location)
	}
	if repoRuns[1].Location != "/tmp" {
		t.Fatalf("expected cwd fallback, got %q", repoRuns[1].Location)
	}

	cwdRuns := buildContextRuns(rows, "cwd", start, end)
	if cwdRuns[0].Location != "/work/repo/sub" {
		t.Fatalf("expected cwd location, got %q", cwdRuns[0].Location)
	}
}

func TestComputeContextSummary(t *testing.T) {
	base := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	runs := []contextRun{
		{StartedAt: base, EndedAt: base.Add(5 * time.Minute), DurationMs: 300000, Location: "repo/a"},
		{StartedAt: base.Add(8 * time.Minute), EndedAt: base.Add(12 * time.Minute), DurationMs: 240000, Location: "repo/a"},
		{StartedAt: base.Add(20 * time.Minute), EndedAt: base.Add(26 * time.Minute), DurationMs: 360000, Location: "repo/b"},
		{StartedAt: base.Add(28 * time.Minute), EndedAt: base.Add(33 * time.Minute), DurationMs: 300000, Location: "repo/a"},
		{StartedAt: base.Add(2 * time.Hour), EndedAt: base.Add(2*time.Hour + 5*time.Minute), DurationMs: 300000, Location: "repo/c"},
	}

	stats := computeContextSummary(runs, 45*time.Minute, 10*time.Minute, 5)
	if stats.TotalRuns != 5 {
		t.Fatalf("expected 5 runs, got %d", stats.TotalRuns)
	}
	if stats.SwitchCount != 2 {
		t.Fatalf("expected 2 switches, got %d", stats.SwitchCount)
	}
	if stats.BounceRate != 0.5 {
		t.Fatalf("expected bounce rate 0.5, got %v", stats.BounceRate)
	}
	if stats.Score <= 0 || stats.Score > 100 {
		t.Fatalf("score out of range: %d", stats.Score)
	}
	if len(stats.TopLocations) == 0 || stats.TopLocations[0].Location != "repo/a" {
		t.Fatalf("expected repo/a as top location, got %+v", stats.TopLocations)
	}
	if len(stats.TopTransitions) != 2 {
		t.Fatalf("expected 2 transition rows, got %d", len(stats.TopTransitions))
	}
	if stats.TopTransitions[0].From != "repo/a" || stats.TopTransitions[0].To != "repo/b" {
		t.Fatalf("unexpected top transition ordering: %+v", stats.TopTransitions)
	}
}

func TestBuildContextTimeline(t *testing.T) {
	base := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	runs := []contextRun{
		{StartedAt: base, EndedAt: base.Add(2 * time.Minute), DurationMs: 120000, Location: "repo/a"},
		{StartedAt: base.Add(5 * time.Minute), EndedAt: base.Add(8 * time.Minute), DurationMs: 180000, Location: "repo/a"},
		{StartedAt: base.Add(20 * time.Minute), EndedAt: base.Add(21 * time.Minute), DurationMs: 60000, Location: "repo/b"},
		{StartedAt: base.Add(40 * time.Minute), EndedAt: base.Add(42 * time.Minute), DurationMs: 120000, Location: "repo/a"},
		{StartedAt: base.Add(2 * time.Hour), EndedAt: base.Add(2*time.Hour + 1*time.Minute), DurationMs: 60000, Location: "repo/c"},
	}

	buckets := buildContextTimeline(runs, 45*time.Minute, "hourly")
	if len(buckets) != 2 {
		t.Fatalf("expected 2 timeline buckets, got %d", len(buckets))
	}
	first := buckets[0]
	if first.Runs != 4 {
		t.Fatalf("expected 4 runs in first bucket, got %d", first.Runs)
	}
	if first.DominantLocation != "repo/a" {
		t.Fatalf("expected dominant repo/a, got %q", first.DominantLocation)
	}
	if first.Switches != 2 {
		t.Fatalf("expected 2 switches in first bucket, got %d", first.Switches)
	}
}
