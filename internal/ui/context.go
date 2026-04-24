package ui

import (
	"fmt"
	"strings"
	"time"
)

type ContextSummaryParams struct {
	Days            int
	LocationMode    string
	Top             int
	IncludeSessions bool
	SuccessOnly     bool
	CwdFilter       string
	GitRootFilter   string
	IdleCutoffMin   int
	BlockGapMin     int
	Start           time.Time
	End             time.Time
}

type ContextSummary struct {
	TotalRuns       int
	TotalWaitMs     float64
	ActiveWallMs    float64
	ActiveHours     float64
	SwitchCount     int
	SwitchesPerHour float64
	BounceRate      float64
	Score           int
	FocusBlockP50Ms float64
	FocusBlockP95Ms float64
	DeepWorkRatio   float64
	TopLocations    []ContextLocationEntry
	TopTransitions  []ContextTransitionEntry
}

type ContextLocationEntry struct {
	Location string
	Runs     int
	WaitMs   float64
	SharePct float64
}

type ContextTransitionEntry struct {
	From              string
	To                string
	Switches          int
	MedianGapMs       float64
	WaitAfterSwitchMs float64
}

type ContextTimelineParams struct {
	Days            int
	LocationMode    string
	Granularity     string
	IncludeSessions bool
	SuccessOnly     bool
	CwdFilter       string
	GitRootFilter   string
	Start           time.Time
	End             time.Time
}

type ContextTimelineBucket struct {
	Start            time.Time
	DominantLocation string
	DominantSharePct float64
	Switches         int
	Runs             int
}

func RenderContextSummaryReport(params ContextSummaryParams, summary ContextSummary) string {
	var b strings.Builder
	b.WriteString("Context Summary\n")
	fmt.Fprintf(&b, "Window: %s -> %s (last %dd)\n",
		params.Start.Format("2006-01-02"),
		params.End.Format("2006-01-02"),
		params.Days,
	)
	fmt.Fprintf(&b, "Scope: location=%s | sessions=%s | inclusion=%s | idle_cutoff=%dm | block_gap=%dm\n",
		params.LocationMode,
		yesNo(params.IncludeSessions),
		inclusionLabel(params.SuccessOnly),
		params.IdleCutoffMin,
		params.BlockGapMin,
	)
	if params.CwdFilter != "" || params.GitRootFilter != "" {
		filters := make([]string, 0, 2)
		if params.CwdFilter != "" {
			filters = append(filters, "cwd~"+params.CwdFilter)
		}
		if params.GitRootFilter != "" {
			filters = append(filters, "git_root="+params.GitRootFilter)
		}
		fmt.Fprintf(&b, "Filters: %s\n", strings.Join(filters, " | "))
	}

	if summary.TotalRuns == 0 {
		b.WriteString("\nNo matching timing data in selected window.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "Runs: %d | Total wait: %s | Active time: %s (%.1fh)\n",
		summary.TotalRuns,
		fmtDuration(summary.TotalWaitMs),
		fmtDuration(summary.ActiveWallMs),
		summary.ActiveHours,
	)

	b.WriteString("\nSwitching\n")
	fmt.Fprintf(&b, "- Switches: %d\n", summary.SwitchCount)
	fmt.Fprintf(&b, "- Switches/hour: %.2f\n", summary.SwitchesPerHour)
	fmt.Fprintf(&b, "- Bounce rate: %.1f%%\n", summary.BounceRate*100)
	fmt.Fprintf(&b, "- Context switching score: %d/100 (higher = more fragmented)\n", summary.Score)

	b.WriteString("\nFocus\n")
	fmt.Fprintf(&b, "- Focus block p50: %s\n", fmtDuration(summary.FocusBlockP50Ms))
	fmt.Fprintf(&b, "- Focus block p95: %s\n", fmtDuration(summary.FocusBlockP95Ms))
	fmt.Fprintf(&b, "- Deep work ratio (>=30m blocks): %.1f%%\n", summary.DeepWorkRatio*100)

	b.WriteString("\nTop locations by command count\n")
	for _, item := range summary.TopLocations {
		fmt.Fprintf(&b, "%-26s %5.1f%% %6d cmds %10s wait\n",
			trimCommand(item.Location, 26),
			item.SharePct,
			item.Runs,
			fmtDuration(item.WaitMs),
		)
	}

	b.WriteString("\nTop transitions\n")
	if len(summary.TopTransitions) == 0 {
		b.WriteString("No location switches in this window.\n")
		return b.String()
	}
	for _, tr := range summary.TopTransitions {
		fmt.Fprintf(&b, "%-26s -> %-26s %4d switches median gap %-8s wait after switch %s\n",
			trimCommand(tr.From, 26),
			trimCommand(tr.To, 26),
			tr.Switches,
			fmtDuration(tr.MedianGapMs),
			fmtDuration(tr.WaitAfterSwitchMs),
		)
	}
	return b.String()
}

func RenderContextTimelineReport(params ContextTimelineParams, buckets []ContextTimelineBucket) string {
	var b strings.Builder
	b.WriteString("Context Timeline\n")
	fmt.Fprintf(&b, "Window: %s -> %s (last %dd)\n",
		params.Start.Format("2006-01-02"),
		params.End.Format("2006-01-02"),
		params.Days,
	)
	fmt.Fprintf(&b, "Scope: location=%s | granularity=%s | sessions=%s | inclusion=%s\n",
		params.LocationMode,
		params.Granularity,
		yesNo(params.IncludeSessions),
		inclusionLabel(params.SuccessOnly),
	)
	if params.CwdFilter != "" || params.GitRootFilter != "" {
		filters := make([]string, 0, 2)
		if params.CwdFilter != "" {
			filters = append(filters, "cwd~"+params.CwdFilter)
		}
		if params.GitRootFilter != "" {
			filters = append(filters, "git_root="+params.GitRootFilter)
		}
		fmt.Fprintf(&b, "Filters: %s\n", strings.Join(filters, " | "))
	}
	if len(buckets) == 0 {
		b.WriteString("\nNo matching timing data in selected window.\n")
		return b.String()
	}

	b.WriteString("\n")
	b.WriteString("bucket              dominant_location             share   switches runs\n")
	for _, item := range buckets {
		fmt.Fprintf(&b, "%-19s %-28s %6.1f%% %8d %4d\n",
			formatTimelineBucket(item.Start, params.Granularity),
			trimCommand(item.DominantLocation, 28),
			item.DominantSharePct,
			item.Switches,
			item.Runs,
		)
	}
	return b.String()
}

func formatTimelineBucket(t time.Time, granularity string) string {
	switch granularity {
	case "daily":
		return t.Format("2006-01-02")
	default:
		return t.Format("2006-01-02 15:00")
	}
}
