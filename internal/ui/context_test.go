package ui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderContextSummaryReport(t *testing.T) {
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	report := RenderContextSummaryReport(ContextSummaryParams{
		Days:            7,
		LocationMode:    "repo",
		Top:             3,
		IncludeSessions: false,
		SuccessOnly:     false,
		IdleCutoffMin:   45,
		BlockGapMin:     10,
		Start:           now.AddDate(0, 0, -7),
		End:             now,
	}, ContextSummary{
		TotalRuns:       100,
		TotalWaitMs:     3600000,
		ActiveWallMs:    7200000,
		ActiveHours:     2,
		SwitchCount:     12,
		SwitchesPerHour: 6,
		BounceRate:      0.25,
		Score:           70,
		FocusBlockP50Ms: 600000,
		FocusBlockP95Ms: 1800000,
		DeepWorkRatio:   0.4,
		TopLocations: []ContextLocationEntry{
			{Location: "repo/a", Runs: 60, WaitMs: 1200000, SharePct: 60},
		},
		TopTransitions: []ContextTransitionEntry{
			{From: "repo/a", To: "repo/b", Switches: 8, MedianGapMs: 90000, WaitAfterSwitchMs: 600000},
		},
	})

	if !strings.Contains(report, "Context Summary") {
		t.Fatalf("missing summary heading")
	}
	if !strings.Contains(report, "Switching") || !strings.Contains(report, "Focus") {
		t.Fatalf("missing key sections")
	}
	if !strings.Contains(report, "repo/a") || !strings.Contains(report, "repo/b") {
		t.Fatalf("missing location/transition rows")
	}
}

func TestRenderContextTimelineReport(t *testing.T) {
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	report := RenderContextTimelineReport(ContextTimelineParams{
		Days:            2,
		LocationMode:    "repo",
		Granularity:     "hourly",
		IncludeSessions: false,
		SuccessOnly:     true,
		Start:           now.AddDate(0, 0, -2),
		End:             now,
	}, []ContextTimelineBucket{
		{
			Start:            time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC),
			DominantLocation: "repo/a",
			DominantSharePct: 75,
			Switches:         2,
			Runs:             4,
		},
	})

	if !strings.Contains(report, "Context Timeline") {
		t.Fatalf("missing timeline heading")
	}
	if !strings.Contains(report, "dominant_location") {
		t.Fatalf("missing table header")
	}
	if !strings.Contains(report, "repo/a") {
		t.Fatalf("missing bucket row")
	}
}
