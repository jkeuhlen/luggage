package app

import (
	"flag"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"luggage/internal/metrics"
	"luggage/internal/store"
	"luggage/internal/ui"
)

const (
	contextDefaultReport        = "summary"
	contextDefaultLocation      = "repo"
	contextDefaultTop           = 10
	contextDefaultGranularity   = "hourly"
	contextDefaultIdleCutoffMin = 45
	contextDefaultBlockGapMin   = 10
	contextBounceWindow         = 10 * time.Minute
	contextDeepWorkThreshold    = 30 * time.Minute
)

type contextRun struct {
	StartedAt  time.Time
	EndedAt    time.Time
	DurationMs float64
	Location   string
}

type contextSegment struct {
	Location string
	Started  time.Time
	Ended    time.Time
	WaitMs   float64
}

type contextSwitchEvent struct {
	From      string
	To        string
	GapMs     float64
	Switched  time.Time
	ToSegment int
}

type contextSummaryStats struct {
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
	TopLocations    []ui.ContextLocationEntry
	TopTransitions  []ui.ContextTransitionEntry
}

func (a *App) runContext(args []string) int {
	cfg, st, err := a.openStore()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	defer st.Close()

	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	report := fs.String("report", contextDefaultReport, "summary|timeline")
	days := fs.Int("days", cfg.DefaultDays, "days to look back")
	locationMode := fs.String("location", contextDefaultLocation, "repo|cwd")
	top := fs.Int("top", contextDefaultTop, "max rows to display in summary lists")
	granularity := fs.String("granularity", contextDefaultGranularity, "hourly|daily (timeline)")
	idleCutoffMin := fs.Int("idle-cutoff-min", contextDefaultIdleCutoffMin, "idle gap that breaks switch detection")
	blockGapMin := fs.Int("block-gap-min", contextDefaultBlockGapMin, "max inter-run gap for focus blocks")
	pwdFilter := fs.String("pwd", "", "cwd prefix filter")
	cwdFilter := fs.String("cwd", "", "cwd prefix filter (alias of --pwd)")
	gitRootFilter := fs.String("git-root", "", "git root filter")
	here := fs.Bool("here", false, "filter to current working directory")
	thisRepo := fs.Bool("this-repo", false, "filter to current git repository root")
	includeSessions := fs.Bool("include-sessions", false, "include long-lived sessions")
	successOnlyDefault := cfg.DefaultInclusion == "success_only"
	successOnly := fs.Bool("success-only", successOnlyDefault, "only include successful runs")

	flagArgs, positionals := splitFlagsAndPositionals(
		args,
		map[string]bool{
			"--here":             true,
			"--this-repo":        true,
			"--include-sessions": true,
			"--success-only":     true,
		},
		map[string]bool{
			"--report":          true,
			"--days":            true,
			"--location":        true,
			"--top":             true,
			"--granularity":     true,
			"--idle-cutoff-min": true,
			"--block-gap-min":   true,
			"--pwd":             true,
			"--cwd":             true,
			"--git-root":        true,
		},
	)
	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	if len(positionals) > 0 {
		fmt.Fprintln(a.Stderr, "usage: luggage context [flags]")
		return 1
	}

	switch *report {
	case "summary", "timeline":
	default:
		fmt.Fprintln(a.Stderr, "--report must be summary|timeline")
		return 1
	}
	if *days <= 0 {
		fmt.Fprintln(a.Stderr, "--days must be > 0")
		return 1
	}
	switch *locationMode {
	case "repo", "cwd":
	default:
		fmt.Fprintln(a.Stderr, "--location must be repo|cwd")
		return 1
	}
	if *top <= 0 {
		fmt.Fprintln(a.Stderr, "--top must be > 0")
		return 1
	}
	switch *granularity {
	case "hourly", "daily":
	default:
		fmt.Fprintln(a.Stderr, "--granularity must be hourly|daily")
		return 1
	}
	if *idleCutoffMin < 0 {
		fmt.Fprintln(a.Stderr, "--idle-cutoff-min must be >= 0")
		return 1
	}
	if *blockGapMin <= 0 {
		fmt.Fprintln(a.Stderr, "--block-gap-min must be > 0")
		return 1
	}

	filters, err := resolveScopeFilters(*pwdFilter, *cwdFilter, *gitRootFilter, *here, *thisRepo)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}

	now := time.Now()
	start := now.AddDate(0, 0, -*days)
	rows, err := st.QueryRuns(store.QueryOptions{
		Start:           start,
		View:            "typed",
		IncludeSessions: *includeSessions,
		SuccessOnly:     *successOnly,
		CwdPrefix:       filters.Cwd,
		GitRoot:         filters.GitRoot,
	})
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}

	runs := buildContextRuns(rows, *locationMode, start, now)
	idleCutoff := time.Duration(*idleCutoffMin) * time.Minute
	blockGap := time.Duration(*blockGapMin) * time.Minute

	if *report == "timeline" {
		timelineBuckets := buildContextTimeline(runs, idleCutoff, *granularity)
		out := ui.RenderContextTimelineReport(ui.ContextTimelineParams{
			Days:            *days,
			LocationMode:    *locationMode,
			Granularity:     *granularity,
			IncludeSessions: *includeSessions,
			SuccessOnly:     *successOnly,
			CwdFilter:       filters.Cwd,
			GitRootFilter:   filters.GitRoot,
			Start:           start,
			End:             now,
		}, timelineBuckets)
		fmt.Fprint(a.Stdout, out)
		return 0
	}

	stats := computeContextSummary(runs, idleCutoff, blockGap, *top)
	out := ui.RenderContextSummaryReport(ui.ContextSummaryParams{
		Days:            *days,
		LocationMode:    *locationMode,
		Top:             *top,
		IncludeSessions: *includeSessions,
		SuccessOnly:     *successOnly,
		CwdFilter:       filters.Cwd,
		GitRootFilter:   filters.GitRoot,
		IdleCutoffMin:   *idleCutoffMin,
		BlockGapMin:     *blockGapMin,
		Start:           start,
		End:             now,
	}, ui.ContextSummary{
		TotalRuns:       stats.TotalRuns,
		TotalWaitMs:     stats.TotalWaitMs,
		ActiveWallMs:    stats.ActiveWallMs,
		ActiveHours:     stats.ActiveHours,
		SwitchCount:     stats.SwitchCount,
		SwitchesPerHour: stats.SwitchesPerHour,
		BounceRate:      stats.BounceRate,
		Score:           stats.Score,
		FocusBlockP50Ms: stats.FocusBlockP50Ms,
		FocusBlockP95Ms: stats.FocusBlockP95Ms,
		DeepWorkRatio:   stats.DeepWorkRatio,
		TopLocations:    stats.TopLocations,
		TopTransitions:  stats.TopTransitions,
	})
	fmt.Fprint(a.Stdout, out)
	return 0
}

func buildContextRuns(rows []store.RunRow, locationMode string, start, end time.Time) []contextRun {
	runs := make([]contextRun, 0, len(rows))
	for _, row := range rows {
		if row.StartedAt.Before(start) || !row.StartedAt.Before(end) {
			continue
		}
		durationMs := row.Duration
		if durationMs < 0 {
			durationMs = 0
		}
		endedAt := row.StartedAt.Add(time.Duration(durationMs * float64(time.Millisecond)))
		if endedAt.Before(row.StartedAt) {
			endedAt = row.StartedAt
		}
		runs = append(runs, contextRun{
			StartedAt:  row.StartedAt,
			EndedAt:    endedAt,
			DurationMs: durationMs,
			Location:   contextLocationForRow(row, locationMode),
		})
	}
	return runs
}

func contextLocationForRow(row store.RunRow, locationMode string) string {
	if locationMode == "cwd" {
		if cwd := strings.TrimSpace(row.Cwd); cwd != "" {
			return cwd
		}
		return "(unknown)"
	}
	if root := strings.TrimSpace(row.GitRoot); root != "" {
		return root
	}
	if cwd := strings.TrimSpace(row.Cwd); cwd != "" {
		return cwd
	}
	return "(unknown)"
}

func computeContextSummary(runs []contextRun, idleCutoff, blockGap time.Duration, top int) contextSummaryStats {
	stats := contextSummaryStats{
		TotalRuns: len(runs),
	}
	if len(runs) == 0 {
		return stats
	}

	segments, switches := buildContextSegments(runs, idleCutoff)
	stats.TotalWaitMs = sumContextWait(runs)
	stats.ActiveWallMs = computeActiveWallMs(runs, idleCutoff)
	if stats.ActiveWallMs > 0 {
		stats.ActiveHours = stats.ActiveWallMs / float64(time.Hour/time.Millisecond)
	}
	stats.SwitchCount = len(switches)
	if stats.ActiveHours > 0 {
		stats.SwitchesPerHour = float64(stats.SwitchCount) / stats.ActiveHours
	}

	bounces := countBounces(switches, contextBounceWindow)
	if stats.SwitchCount > 0 {
		stats.BounceRate = float64(bounces) / float64(stats.SwitchCount)
	}

	focusDurationsMs := computeFocusBlockDurationsMs(runs, blockGap)
	if len(focusDurationsMs) > 0 {
		stats.FocusBlockP50Ms = pct(focusDurationsMs, 50)
		stats.FocusBlockP95Ms = pct(focusDurationsMs, 95)
	}
	stats.DeepWorkRatio = computeDeepWorkRatio(focusDurationsMs, contextDeepWorkThreshold)
	stats.Score = contextSwitchingScore(stats.SwitchesPerHour, stats.BounceRate, stats.DeepWorkRatio)

	stats.TopLocations = computeTopLocations(runs, top)
	stats.TopTransitions = computeTopTransitions(segments, switches, top)
	return stats
}

func buildContextSegments(runs []contextRun, idleCutoff time.Duration) ([]contextSegment, []contextSwitchEvent) {
	if len(runs) == 0 {
		return nil, nil
	}
	segments := make([]contextSegment, 0, len(runs))
	switches := make([]contextSwitchEvent, 0, len(runs))

	current := contextSegment{
		Location: runs[0].Location,
		Started:  runs[0].StartedAt,
		Ended:    runs[0].EndedAt,
		WaitMs:   runs[0].DurationMs,
	}
	currentIdx := 0

	for i := 1; i < len(runs); i++ {
		prev := runs[i-1]
		cur := runs[i]
		gap := cur.StartedAt.Sub(prev.EndedAt)
		if gap < 0 {
			gap = 0
		}

		if cur.Location == prev.Location && gap <= idleCutoff {
			current.Ended = maxTime(current.Ended, cur.EndedAt)
			current.WaitMs += cur.DurationMs
			continue
		}

		segments = append(segments, current)
		if cur.Location != prev.Location && gap <= idleCutoff {
			switches = append(switches, contextSwitchEvent{
				From:      prev.Location,
				To:        cur.Location,
				GapMs:     float64(gap) / float64(time.Millisecond),
				Switched:  cur.StartedAt,
				ToSegment: currentIdx + 1,
			})
		}
		currentIdx++
		current = contextSegment{
			Location: cur.Location,
			Started:  cur.StartedAt,
			Ended:    cur.EndedAt,
			WaitMs:   cur.DurationMs,
		}
	}

	segments = append(segments, current)
	return segments, switches
}

func sumContextWait(runs []contextRun) float64 {
	total := 0.0
	for _, run := range runs {
		total += run.DurationMs
	}
	return total
}

func computeActiveWallMs(runs []contextRun, idleCutoff time.Duration) float64 {
	if len(runs) == 0 {
		return 0
	}
	segmentStart := runs[0].StartedAt
	segmentEnd := runs[0].EndedAt
	total := 0.0

	for i := 1; i < len(runs); i++ {
		gap := runs[i].StartedAt.Sub(runs[i-1].EndedAt)
		if gap < 0 {
			gap = 0
		}
		if gap > idleCutoff {
			total += float64(segmentEnd.Sub(segmentStart)) / float64(time.Millisecond)
			segmentStart = runs[i].StartedAt
			segmentEnd = runs[i].EndedAt
			continue
		}
		segmentEnd = maxTime(segmentEnd, runs[i].EndedAt)
	}
	total += float64(segmentEnd.Sub(segmentStart)) / float64(time.Millisecond)
	return total
}

func computeFocusBlockDurationsMs(runs []contextRun, blockGap time.Duration) []float64 {
	if len(runs) == 0 {
		return nil
	}
	durations := make([]float64, 0, len(runs))

	blockStart := runs[0].StartedAt
	blockEnd := runs[0].EndedAt
	for i := 1; i < len(runs); i++ {
		prev := runs[i-1]
		cur := runs[i]
		gap := cur.StartedAt.Sub(prev.EndedAt)
		if gap < 0 {
			gap = 0
		}
		if cur.Location == prev.Location && gap <= blockGap {
			blockEnd = maxTime(blockEnd, cur.EndedAt)
			continue
		}
		durations = append(durations, float64(blockEnd.Sub(blockStart))/float64(time.Millisecond))
		blockStart = cur.StartedAt
		blockEnd = cur.EndedAt
	}
	durations = append(durations, float64(blockEnd.Sub(blockStart))/float64(time.Millisecond))
	return durations
}

func computeDeepWorkRatio(blockDurationsMs []float64, threshold time.Duration) float64 {
	if len(blockDurationsMs) == 0 {
		return 0
	}
	thresholdMs := float64(threshold / time.Millisecond)
	total := 0.0
	deep := 0.0
	for _, d := range blockDurationsMs {
		total += d
		if d >= thresholdMs {
			deep += d
		}
	}
	if total <= 0 {
		return 0
	}
	return deep / total
}

func contextSwitchingScore(switchesPerHour, bounceRate, deepWorkRatio float64) int {
	switchComponent := clamp01(switchesPerHour/4.0) * 60.0
	bounceComponent := clamp01(bounceRate) * 25.0
	deepPenalty := clamp01(1.0-deepWorkRatio) * 15.0
	score := int(math.Round(switchComponent + bounceComponent + deepPenalty))
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func countBounces(switches []contextSwitchEvent, window time.Duration) int {
	if len(switches) < 2 {
		return 0
	}
	count := 0
	for i := 0; i+1 < len(switches); i++ {
		cur := switches[i]
		next := switches[i+1]
		if next.From == cur.To && next.To == cur.From && next.Switched.Sub(cur.Switched) <= window {
			count++
		}
	}
	return count
}

func computeTopLocations(runs []contextRun, top int) []ui.ContextLocationEntry {
	type agg struct {
		runs int
		wait float64
	}
	groups := make(map[string]*agg)
	for _, run := range runs {
		entry := groups[run.Location]
		if entry == nil {
			entry = &agg{}
			groups[run.Location] = entry
		}
		entry.runs++
		entry.wait += run.DurationMs
	}

	totalRuns := len(runs)
	out := make([]ui.ContextLocationEntry, 0, len(groups))
	for location, entry := range groups {
		share := 0.0
		if totalRuns > 0 {
			share = (float64(entry.runs) / float64(totalRuns)) * 100
		}
		out = append(out, ui.ContextLocationEntry{
			Location: location,
			Runs:     entry.runs,
			WaitMs:   entry.wait,
			SharePct: share,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runs == out[j].Runs {
			if out[i].WaitMs == out[j].WaitMs {
				return out[i].Location < out[j].Location
			}
			return out[i].WaitMs > out[j].WaitMs
		}
		return out[i].Runs > out[j].Runs
	})
	if top > 0 && len(out) > top {
		out = out[:top]
	}
	return out
}

func computeTopTransitions(segments []contextSegment, switches []contextSwitchEvent, top int) []ui.ContextTransitionEntry {
	type key struct {
		from string
		to   string
	}
	type agg struct {
		switches int
		gaps     []float64
		wait     float64
	}

	groups := make(map[key]*agg)
	for _, sw := range switches {
		k := key{from: sw.From, to: sw.To}
		entry := groups[k]
		if entry == nil {
			entry = &agg{}
			groups[k] = entry
		}
		entry.switches++
		entry.gaps = append(entry.gaps, sw.GapMs)
		if sw.ToSegment >= 0 && sw.ToSegment < len(segments) {
			entry.wait += segments[sw.ToSegment].WaitMs
		}
	}

	out := make([]ui.ContextTransitionEntry, 0, len(groups))
	for k, entry := range groups {
		out = append(out, ui.ContextTransitionEntry{
			From:              k.from,
			To:                k.to,
			Switches:          entry.switches,
			MedianGapMs:       pct(entry.gaps, 50),
			WaitAfterSwitchMs: entry.wait,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Switches == out[j].Switches {
			if out[i].WaitAfterSwitchMs == out[j].WaitAfterSwitchMs {
				if out[i].From == out[j].From {
					return out[i].To < out[j].To
				}
				return out[i].From < out[j].From
			}
			return out[i].WaitAfterSwitchMs > out[j].WaitAfterSwitchMs
		}
		return out[i].Switches > out[j].Switches
	})
	if top > 0 && len(out) > top {
		out = out[:top]
	}
	return out
}

func buildContextTimeline(runs []contextRun, idleCutoff time.Duration, granularity string) []ui.ContextTimelineBucket {
	if len(runs) == 0 {
		return nil
	}
	bucketGranularity := metrics.Hourly
	if granularity == "daily" {
		bucketGranularity = metrics.Daily
	}
	loc := runs[0].StartedAt.Location()

	type bucketAgg struct {
		start    time.Time
		runs     int
		switches int
		byLoc    map[string]int
	}
	bucketMap := map[int64]*bucketAgg{}
	getBucket := func(ts time.Time) *bucketAgg {
		bs := metrics.BucketStart(ts, bucketGranularity, loc)
		key := bs.Unix()
		entry := bucketMap[key]
		if entry == nil {
			entry = &bucketAgg{start: bs, byLoc: map[string]int{}}
			bucketMap[key] = entry
		}
		return entry
	}

	for _, run := range runs {
		b := getBucket(run.StartedAt)
		b.runs++
		b.byLoc[run.Location]++
	}
	_, switches := buildContextSegments(runs, idleCutoff)
	for _, sw := range switches {
		b := getBucket(sw.Switched)
		b.switches++
	}

	keys := make([]int64, 0, len(bucketMap))
	for k := range bucketMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	out := make([]ui.ContextTimelineBucket, 0, len(keys))
	for _, k := range keys {
		b := bucketMap[k]
		if b.runs == 0 {
			continue
		}
		dominantLoc := ""
		dominantRuns := 0
		for location, runs := range b.byLoc {
			if runs > dominantRuns || (runs == dominantRuns && (dominantLoc == "" || location < dominantLoc)) {
				dominantLoc = location
				dominantRuns = runs
			}
		}
		share := 0.0
		if b.runs > 0 {
			share = (float64(dominantRuns) / float64(b.runs)) * 100
		}
		out = append(out, ui.ContextTimelineBucket{
			Start:            b.start,
			DominantLocation: dominantLoc,
			DominantSharePct: share,
			Switches:         b.switches,
			Runs:             b.runs,
		})
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
