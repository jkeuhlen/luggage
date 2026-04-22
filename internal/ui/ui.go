package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"luggage/internal/metrics"
)

type TimeReportParams struct {
	Command         string
	View            string
	Granularity     metrics.Granularity
	Days            int
	Window          string
	IncludeSessions bool
	SuccessOnly     bool
	CwdFilter       string
	GitRootFilter   string
}

func RenderTimeReport(params TimeReportParams, buckets []metrics.Bucket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Command: %s\n", params.Command)
	fmt.Fprintf(&b, "View: %s | Granularity: %s | Days: %d | Window: %s | Sessions: %s | Inclusion: %s\n",
		params.View,
		params.Granularity,
		params.Days,
		params.Window,
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

	runs, meanMs, medianMs, p95Ms := metrics.Summary(buckets)
	fmt.Fprintf(&b, "Runs: %d | Mean: %s | Median: %s | P95: %s\n", runs, fmtMS(meanMs), fmtMS(medianMs), fmtMS(p95Ms))

	nonEmpty := nonEmptyBuckets(buckets)
	if len(nonEmpty) == 0 {
		b.WriteString("\nNo matching timing data in selected window.\n")
		return b.String()
	}

	displayBuckets := nonEmpty
	if params.Window == "full" {
		displayBuckets = buckets
	}

	firstActive := nonEmpty[0].Start.Format("2006-01-02")
	lastActive := nonEmpty[len(nonEmpty)-1].Start.Format("2006-01-02")
	fmt.Fprintf(&b, "Active points: %d buckets with runs (%s to %s)\n", len(nonEmpty), firstActive, lastActive)

	if len(nonEmpty) < 3 {
		b.WriteString("\nTrend chart hidden (need at least 3 active buckets).\n")
	} else {
		b.WriteString("\nTrend (unicode sparkline; higher is slower)\n")
		b.WriteString(renderSparklineChart(displayBuckets, params.Granularity, 60))
	}

	startRecent := 0
	if len(nonEmpty) > 8 {
		startRecent = len(nonEmpty) - 8
	}
	b.WriteString("\nRecent buckets\n")
	for i := startRecent; i < len(nonEmpty); i++ {
		row := nonEmpty[i]
		delta := "-"
		if i > 0 {
			delta = fmtSignedMS(row.MedianMs - nonEmpty[i-1].MedianMs)
		}
		mark := ""
		if row.Anomaly {
			mark = " !"
		}
		fmt.Fprintf(&b, "- %s median=%-8s p95=%-8s n=%-4d delta=%-8s%s\n",
			bucketLabel(row.Start, params.Granularity),
			fmtMS(row.MedianMs),
			fmtMS(row.P95Ms),
			row.Count,
			delta,
			mark,
		)
	}

	anoms := make([]metrics.Bucket, 0)
	for _, row := range nonEmpty {
		if row.Anomaly {
			anoms = append(anoms, row)
		}
	}
	if len(anoms) > 0 {
		sort.Slice(anoms, func(i, j int) bool {
			return anoms[i].AnomalyFactor > anoms[j].AnomalyFactor
		})
		limit := min(3, len(anoms))
		b.WriteString("\nPotential regression points\n")
		for i := 0; i < limit; i++ {
			row := anoms[i]
			fmt.Fprintf(&b,
				"- %s median %s vs baseline %s (%.2fx)\n",
				row.Start.Format("2006-01-02"),
				fmtMS(row.MedianMs),
				fmtMS(row.BaselineMs),
				row.AnomalyFactor,
			)
		}
	}
	return b.String()
}

type seriesPoint struct {
	Start  time.Time
	End    time.Time
	Count  int
	Median float64
	P95    float64
}

func renderSparklineChart(buckets []metrics.Bucket, g metrics.Granularity, maxWidth int) string {
	points, bucketSpan := buildSeriesPoints(buckets, maxWidth)
	if len(points) == 0 {
		return "No chart data.\n"
	}

	medianVals := make([]float64, len(points))
	p95Vals := make([]float64, len(points))
	minMedian := math.MaxFloat64
	maxMedian := 0.0
	for i, p := range points {
		medianVals[i] = p.Median
		p95Vals[i] = p.P95
		if p.Count == 0 {
			continue
		}
		if p.Median < minMedian {
			minMedian = p.Median
		}
		if p.Median > maxMedian {
			maxMedian = p.Median
		}
	}
	if minMedian == math.MaxFloat64 {
		return "No chart data.\n"
	}

	medLine := sparkline(medianVals, minMedian, maxMedian, points)
	p95GapLine := p95GapMarkers(points)

	var b strings.Builder
	if bucketSpan > 1 {
		fmt.Fprintf(&b, "Compressed: 1 column ~= %d buckets\n", bucketSpan)
	}
	fmt.Fprintf(&b, "median |%s|\n", medLine)
	fmt.Fprintf(&b, "p95↑   |%s|\n", p95GapLine)
	fmt.Fprintf(&b, "ticks  |%s|\n", tickLine(len(points)))
	if len(points) < 24 {
		fmt.Fprintf(&b, "dates  start=%s end=%s\n",
			bucketLabel(points[0].Start, g),
			bucketLabel(points[len(points)-1].End, g),
		)
	} else {
		fmt.Fprintf(&b, "dates  %s\n", dateLabelLine(points, g))
	}
	fmt.Fprintf(&b, "scale  median %s -> %s\n", fmtMS(minMedian), fmtMS(maxMedian))
	fmt.Fprintf(&b, "range  %s -> %s\n",
		bucketLabel(points[0].Start, g),
		bucketLabel(points[len(points)-1].End, g),
	)
	return b.String()
}

func buildSeriesPoints(buckets []metrics.Bucket, maxWidth int) ([]seriesPoint, int) {
	if len(buckets) == 0 {
		return nil, 1
	}
	if maxWidth < 8 {
		maxWidth = 8
	}
	bucketCount := len(buckets)
	width := bucketCount
	if width > maxWidth {
		width = maxWidth
	}
	points := make([]seriesPoint, 0, width)
	bucketSpan := int(math.Ceil(float64(bucketCount) / float64(width)))
	if bucketSpan < 1 {
		bucketSpan = 1
	}

	for i := 0; i < width; i++ {
		startIdx := i * bucketSpan
		if startIdx >= bucketCount {
			break
		}
		endIdx := (i+1)*bucketSpan - 1
		if endIdx >= bucketCount {
			endIdx = bucketCount - 1
		}

		p := seriesPoint{Start: buckets[startIdx].Start, End: buckets[endIdx].Start}
		durations := make([]float64, 0)
		for j := startIdx; j <= endIdx; j++ {
			if buckets[j].Count == 0 {
				continue
			}
			p.Count += buckets[j].Count
			durations = append(durations, buckets[j].DurationsMs...)
		}
		if len(durations) > 0 {
			p.Median = percentile(durations, 50)
			p.P95 = percentile(durations, 95)
		}
		points = append(points, p)
	}
	return points, bucketSpan
}

func sparkline(values []float64, minValue, maxValue float64, points []seriesPoint) string {
	if len(values) == 0 || len(points) != len(values) {
		return ""
	}
	levels := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var b strings.Builder
	rangeValue := maxValue - minValue
	for i, v := range values {
		if points[i].Count == 0 {
			b.WriteRune('·')
			continue
		}
		if rangeValue <= 0 {
			b.WriteRune('▄')
			continue
		}
		raw := ((v - minValue) / rangeValue) * float64(len(levels)-1)
		idx := int(math.Round(raw))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(levels) {
			idx = len(levels) - 1
		}
		b.WriteRune(levels[idx])
	}
	return b.String()
}

func p95GapMarkers(points []seriesPoint) string {
	var b strings.Builder
	for _, p := range points {
		if p.Count == 0 {
			b.WriteRune('·')
			continue
		}
		if p.Median <= 0 {
			b.WriteRune('▲')
			continue
		}
		gap := (p.P95 - p.Median) / p.Median
		switch {
		case gap < 0.10:
			b.WriteRune('·')
		case gap < 0.25:
			b.WriteRune('▴')
		case gap < 0.50:
			b.WriteRune('▲')
		default:
			b.WriteRune('◆')
		}
	}
	return b.String()
}

func tickLine(width int) string {
	if width <= 0 {
		return ""
	}
	line := make([]rune, width)
	for i := range line {
		line[i] = '─'
	}
	positions := tickPositions(width)
	for _, pos := range positions {
		line[pos] = '┬'
	}
	return string(line)
}

func dateLabelLine(points []seriesPoint, g metrics.Granularity) string {
	width := len(points)
	if width <= 0 {
		return ""
	}
	line := make([]rune, width)
	for i := range line {
		line[i] = ' '
	}
	pos := tickPositions(width)
	labels := []string{
		bucketLabel(points[0].Start, g),
		bucketLabel(points[pos[1]].Start, g),
		bucketLabel(points[pos[2]].Start, g),
		bucketLabel(points[pos[3]].Start, g),
		bucketLabel(points[width-1].End, g),
	}
	for i, p := range pos {
		placeLabel(line, p, labels[i], i == 0, i == len(pos)-1)
	}
	return string(line)
}

func tickPositions(width int) []int {
	if width <= 1 {
		return []int{0, 0, 0, 0, 0}
	}
	return []int{
		0,
		width / 4,
		width / 2,
		(3 * width) / 4,
		width - 1,
	}
}

func placeLabel(line []rune, center int, label string, leftAnchored, rightAnchored bool) {
	if len(line) == 0 || label == "" {
		return
	}
	labelRunes := []rune(label)
	start := center - (len(labelRunes) / 2)
	if leftAnchored {
		start = 0
	}
	if rightAnchored {
		start = len(line) - len(labelRunes)
	}
	if start < 0 {
		start = 0
	}
	if start+len(labelRunes) > len(line) {
		start = len(line) - len(labelRunes)
	}
	if start < 0 {
		return
	}
	for i, r := range labelRunes {
		idx := start + i
		if idx < 0 || idx >= len(line) {
			continue
		}
		line[idx] = r
	}
}

func nonEmptyBuckets(in []metrics.Bucket) []metrics.Bucket {
	out := make([]metrics.Bucket, 0, len(in))
	for _, b := range in {
		if b.Count > 0 {
			out = append(out, b)
		}
	}
	return out
}

func percentile(in []float64, p int) float64 {
	if len(in) == 0 {
		return 0
	}
	clone := append([]float64(nil), in...)
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

type SessionSummary struct {
	Days       int
	Runs       int
	TotalMs    float64
	MedianMs   float64
	P95Ms      float64
	TopEntries []SessionEntry
}

type SessionEntry struct {
	Command string
	Runs    int
	TotalMs float64
	MeanMs  float64
	P95Ms   float64
}

func RenderSessionSummary(summary SessionSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Sessions window: last %d days\n", summary.Days)
	fmt.Fprintf(&b, "Sessions: %d | Total: %s | Median: %s | P95: %s\n",
		summary.Runs,
		fmtDuration(summary.TotalMs),
		fmtMS(summary.MedianMs),
		fmtMS(summary.P95Ms),
	)
	if len(summary.TopEntries) == 0 {
		b.WriteString("No long-lived session commands found.\n")
		return b.String()
	}
	b.WriteString("\nTop session commands by total time\n")
	for _, item := range summary.TopEntries {
		fmt.Fprintf(&b, "- %-20s runs=%-4d total=%-10s mean=%-8s p95=%s\n",
			item.Command,
			item.Runs,
			fmtDuration(item.TotalMs),
			fmtMS(item.MeanMs),
			fmtMS(item.P95Ms),
		)
	}
	return b.String()
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func inclusionLabel(successOnly bool) string {
	if successOnly {
		return "success-only"
	}
	return "all-runs"
}

func bucketLabel(t time.Time, g metrics.Granularity) string {
	switch g {
	case metrics.Hourly:
		return t.Format("2006-01-02 15:00")
	case metrics.Weekly:
		return "wk " + t.Format("2006-01-02")
	default:
		return t.Format("2006-01-02")
	}
}

func fmtMS(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.2fs", ms/1000)
	}
	return fmt.Sprintf("%.0fms", ms)
}

func fmtSignedMS(ms float64) string {
	if ms > 0 {
		if ms >= 1000 {
			return fmt.Sprintf("+%.2fs", ms/1000)
		}
		return fmt.Sprintf("+%.0fms", ms)
	}
	if ms < 0 {
		if ms <= -1000 {
			return fmt.Sprintf("%.2fs", ms/1000)
		}
		return fmt.Sprintf("%.0fms", ms)
	}
	return "0ms"
}

func fmtDuration(ms float64) string {
	d := time.Duration(ms * float64(time.Millisecond))
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd%dh", days, hours)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
