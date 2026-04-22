package metrics

import (
	"math"
	"sort"
	"time"

	"luggage/internal/store"
)

type Bucket struct {
	Start         time.Time
	Count         int
	MeanMs        float64
	MedianMs      float64
	P95Ms         float64
	DurationsMs   []float64
	Anomaly       bool
	BaselineMs    float64
	AnomalyFactor float64
}

type Granularity string

const (
	Hourly Granularity = "hourly"
	Daily  Granularity = "daily"
	Weekly Granularity = "weekly"
)

func NormalizeGranularity(v string) Granularity {
	switch v {
	case string(Hourly):
		return Hourly
	case string(Weekly):
		return Weekly
	default:
		return Daily
	}
}

func BucketStart(t time.Time, g Granularity, loc *time.Location) time.Time {
	t = t.In(loc)
	switch g {
	case Hourly:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
	case Weekly:
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := t.AddDate(0, 0, -(weekday - 1))
		return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	default:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
}

func NextBucketStart(t time.Time, g Granularity) time.Time {
	switch g {
	case Hourly:
		return t.Add(time.Hour)
	case Weekly:
		return t.AddDate(0, 0, 7)
	default:
		return t.AddDate(0, 0, 1)
	}
}

func BuildBuckets(rows []store.RunRow, start, end time.Time, g Granularity, loc *time.Location) []Bucket {
	start = BucketStart(start, g, loc)
	end = BucketStart(end, g, loc)

	bucketMap := make(map[int64]*Bucket)
	for ts := start; !ts.After(end); ts = NextBucketStart(ts, g) {
		b := Bucket{Start: ts}
		bucketMap[ts.Unix()] = &b
	}

	for _, row := range rows {
		bs := BucketStart(row.StartedAt, g, loc)
		k := bs.Unix()
		b, ok := bucketMap[k]
		if !ok {
			copy := Bucket{Start: bs}
			bucketMap[k] = &copy
			b = &copy
		}
		b.DurationsMs = append(b.DurationsMs, row.Duration)
	}

	out := make([]Bucket, 0, len(bucketMap))
	for ts := start; !ts.After(end); ts = NextBucketStart(ts, g) {
		b := bucketMap[ts.Unix()]
		if b == nil {
			b = &Bucket{Start: ts}
		}
		b.Count = len(b.DurationsMs)
		if b.Count > 0 {
			b.MeanMs = mean(b.DurationsMs)
			b.MedianMs = percentile(b.DurationsMs, 50)
			b.P95Ms = percentile(b.DurationsMs, 95)
		}
		out = append(out, *b)
	}
	return out
}

func DetectAnomalies(buckets []Bucket, window int, sigma float64) {
	if window <= 1 || sigma <= 0 {
		return
	}
	for i := range buckets {
		if buckets[i].Count == 0 {
			continue
		}
		series := make([]float64, 0, window)
		for j := i - 1; j >= 0 && len(series) < window; j-- {
			if buckets[j].Count > 0 {
				series = append(series, buckets[j].MedianMs)
			}
		}
		if len(series) < max(5, window/2) {
			continue
		}
		baseline := mean(series)
		std := stddev(series, baseline)
		threshold := baseline + sigma*std
		current := buckets[i].MedianMs
		if current > threshold && current > baseline*1.15 {
			buckets[i].Anomaly = true
			buckets[i].BaselineMs = baseline
			if baseline > 0 {
				buckets[i].AnomalyFactor = current / baseline
			}
		}
	}
}

func Summary(buckets []Bucket) (runs int, meanMs, medianMs, p95Ms float64) {
	all := make([]float64, 0)
	for _, b := range buckets {
		runs += b.Count
		if b.Count > 0 {
			all = append(all, b.DurationsMs...)
		}
	}
	if len(all) == 0 {
		return runs, 0, 0, 0
	}
	meanMs = mean(all)
	medianMs = percentile(all, 50)
	p95Ms = percentile(all, 95)
	return runs, meanMs, medianMs, p95Ms
}

func mean(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range in {
		sum += x
	}
	return sum / float64(len(in))
}

func stddev(in []float64, m float64) float64 {
	if len(in) < 2 {
		return 0
	}
	var sum float64
	for _, x := range in {
		d := x - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(in)-1))
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
