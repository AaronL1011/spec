package analytics

import (
	"sort"
	"strings"
	"time"
)

// Distribution summarises a set of duration samples with the percentiles that
// matter for flow analysis. Averages are deliberately absent: lead-time data
// is long-tailed and averages mislead.
type Distribution struct {
	Samples []time.Duration
	P50     time.Duration
	P85     time.Duration
	P95     time.Duration
}

// NewDistribution computes percentiles over the given samples.
func NewDistribution(samples []time.Duration) Distribution {
	d := Distribution{Samples: samples}
	if len(samples) == 0 {
		return d
	}
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	d.P50 = percentile(sorted, 0.50)
	d.P85 = percentile(sorted, 0.85)
	d.P95 = percentile(sorted, 0.95)
	return d
}

// Count returns the sample count.
func (d Distribution) Count() int { return len(d.Samples) }

// percentile returns the nearest-rank percentile of pre-sorted samples.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(p*float64(len(sorted)) + 0.9999)
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// sparkRunes are the eight block heights used for histograms, low to high.
var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// sparklineBuckets is the fixed histogram width for distribution sparklines.
const sparklineBuckets = 8

// Sparkline renders a fixed-width histogram of the samples. Empty input
// yields an empty string.
func Sparkline(samples []time.Duration) string {
	if len(samples) == 0 {
		return ""
	}
	minS, maxS := samples[0], samples[0]
	for _, s := range samples {
		if s < minS {
			minS = s
		}
		if s > maxS {
			maxS = s
		}
	}

	counts := make([]int, sparklineBuckets)
	span := maxS - minS
	for _, s := range samples {
		idx := 0
		if span > 0 {
			idx = int(float64(s-minS) / float64(span) * float64(sparklineBuckets-1))
		}
		counts[idx]++
	}

	peak := 0
	for _, c := range counts {
		if c > peak {
			peak = c
		}
	}

	var sb strings.Builder
	for _, c := range counts {
		if c == 0 {
			sb.WriteRune(' ')
			continue
		}
		level := (c*(len(sparkRunes)-1) + peak - 1) / peak
		sb.WriteRune(sparkRunes[level])
	}
	return strings.TrimRight(sb.String(), " ")
}
