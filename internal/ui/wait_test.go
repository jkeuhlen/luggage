package ui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderWaitReport(t *testing.T) {
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	params := WaitReportParams{
		Days:            30,
		Limit:           3,
		View:            "typed",
		Compare:         true,
		CompareDays:     30,
		IncludeSessions: false,
		SuccessOnly:     false,
		CurrentStart:    now.AddDate(0, 0, -30),
		CurrentEnd:      now,
		PrevStart:       now.AddDate(0, 0, -60),
		PrevEnd:         now.AddDate(0, 0, -30),
	}
	entries := []WaitEntry{
		{
			Command:        "git",
			CurrentTotalMs: 5000,
			CurrentRuns:    10,
			CurrentMeanMs:  500,
			PrevTotalMs:    3000,
			PrevRuns:       8,
			PrevMeanMs:     375,
			Subcommands: []WaitSubEntry{
				{Command: "git status", TotalMs: 2000, Runs: 4, MeanMs: 500},
				{Command: "git fetch", TotalMs: 1500, Runs: 2, MeanMs: 750},
			},
		},
		{Command: "npm", CurrentTotalMs: 2000, CurrentRuns: 4, CurrentMeanMs: 500, PrevTotalMs: 0, PrevRuns: 0, PrevMeanMs: 0},
	}
	totals := WaitTotals{CurrentTotalMs: 7000, CurrentRuns: 14, PrevTotalMs: 3000, PrevRuns: 8}

	report := RenderWaitReport(params, entries, totals)
	if !strings.Contains(report, "Waiting Time Breakdown") {
		t.Fatalf("missing heading")
	}
	if !strings.Contains(report, "Top commands by total wait") {
		t.Fatalf("missing table heading")
	}
	if !strings.Contains(report, "git") || !strings.Contains(report, "npm") {
		t.Fatalf("missing command rows")
	}
	if !strings.Contains(report, "new") {
		t.Fatalf("expected 'new' label for no previous period data")
	}
	if !strings.Contains(report, "git status") || !strings.Contains(report, "git fetch") {
		t.Fatalf("expected subcommand rows")
	}
}
