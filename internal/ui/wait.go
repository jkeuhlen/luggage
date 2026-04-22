package ui

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type WaitReportParams struct {
	Days            int
	Limit           int
	View            string
	Compare         bool
	CompareDays     int
	IncludeSessions bool
	SuccessOnly     bool
	CwdFilter       string
	GitRootFilter   string
	CurrentStart    time.Time
	CurrentEnd      time.Time
	PrevStart       time.Time
	PrevEnd         time.Time
}

type WaitEntry struct {
	Command        string
	CurrentTotalMs float64
	CurrentRuns    int
	CurrentMeanMs  float64
	PrevTotalMs    float64
	PrevRuns       int
	PrevMeanMs     float64
	Subcommands    []WaitSubEntry
}

type WaitSubEntry struct {
	Command string
	TotalMs float64
	Runs    int
	MeanMs  float64
}

type WaitTotals struct {
	CurrentTotalMs float64
	CurrentRuns    int
	PrevTotalMs    float64
	PrevRuns       int
}

func RenderWaitReport(params WaitReportParams, entries []WaitEntry, totals WaitTotals) string {
	var b strings.Builder
	b.WriteString("Waiting Time Breakdown\n")
	fmt.Fprintf(&b, "Window: %s -> %s (last %dd)\n",
		params.CurrentStart.Format("2006-01-02"),
		params.CurrentEnd.Format("2006-01-02"),
		params.Days,
	)
	fmt.Fprintf(&b, "Scope: view=%s | sessions=%s | inclusion=%s\n",
		params.View,
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
	fmt.Fprintf(&b, "Current total wait: %s across %d runs\n", fmtDuration(totals.CurrentTotalMs), totals.CurrentRuns)
	if params.Compare {
		fmt.Fprintf(&b, "Compare window: %s -> %s (%dd prior)\n",
			params.PrevStart.Format("2006-01-02"),
			params.PrevEnd.Format("2006-01-02"),
			params.CompareDays,
		)
		fmt.Fprintf(&b, "Previous total wait: %s across %d runs\n", fmtDuration(totals.PrevTotalMs), totals.PrevRuns)
	}

	if len(entries) == 0 {
		b.WriteString("\nNo matching timing data in current window.\n")
		return b.String()
	}

	b.WriteString("\nTop commands by total wait\n")
	if params.Compare {
		b.WriteString("cmd                  current    prev       delta      change     runs(c/p)\n")
		for _, e := range entries {
			delta := e.CurrentTotalMs - e.PrevTotalMs
			fmt.Fprintf(&b, "%-20s %-10s %-10s %-10s %-10s %d/%d\n",
				trimCommand(e.Command, 20),
				fmtDuration(e.CurrentTotalMs),
				fmtDuration(e.PrevTotalMs),
				fmtSignedDuration(delta),
				pctChangeLabel(e.CurrentTotalMs, e.PrevTotalMs),
				e.CurrentRuns,
				e.PrevRuns,
			)
			writeSubcommandBreakdown(&b, e.Subcommands, e.CurrentTotalMs)
		}
		return b.String()
	}

	b.WriteString("cmd                  total       share    runs   avg\n")
	for _, e := range entries {
		share := 0.0
		if totals.CurrentTotalMs > 0 {
			share = (e.CurrentTotalMs / totals.CurrentTotalMs) * 100
		}
		fmt.Fprintf(&b, "%-20s %-10s %6.1f%% %6d %-8s\n",
			trimCommand(e.Command, 20),
			fmtDuration(e.CurrentTotalMs),
			share,
			e.CurrentRuns,
			fmtMS(e.CurrentMeanMs),
		)
		writeSubcommandBreakdown(&b, e.Subcommands, e.CurrentTotalMs)
	}
	return b.String()
}

func pctChangeLabel(current, prev float64) string {
	if prev <= 0 {
		if current > 0 {
			return "new"
		}
		return "-"
	}
	pct := ((current - prev) / prev) * 100
	return fmt.Sprintf("%+.1f%%", pct)
}

func trimCommand(cmd string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(cmd)
	if len(r) <= n {
		return cmd
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func writeSubcommandBreakdown(b *strings.Builder, subs []WaitSubEntry, parentTotalMs float64) {
	if len(subs) == 0 {
		return
	}
	for _, s := range subs {
		share := 0.0
		if parentTotalMs > 0 {
			share = (s.TotalMs / parentTotalMs) * 100
		}
		fmt.Fprintf(b, "  -> %-22s %-10s %6.1f%% runs=%-4d avg=%s\n",
			trimCommand(s.Command, 22),
			fmtDuration(s.TotalMs),
			share,
			s.Runs,
			fmtMS(s.MeanMs),
		)
	}
}

func fmtSignedDuration(ms float64) string {
	if math.Abs(ms) < 0.5 {
		return "0s"
	}
	if ms > 0 {
		return "+" + fmtDuration(ms)
	}
	return "-" + fmtDuration(-ms)
}
