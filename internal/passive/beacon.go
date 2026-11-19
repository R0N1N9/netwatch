package passive

import "math"

// BeaconVerdict describes periodicity of contact intervals.
type BeaconVerdict struct {
	Samples  int
	MeanSecs float64
	CV       float64 // coefficient of variation (std/mean); low == regular
	Beacon   bool
	Score    float64 // 0-100
}

// AnalyzeBeacon inspects inter-contact intervals (seconds) for C2-style
// periodicity. Regular, low-jitter callbacks score high.
func AnalyzeBeacon(intervals []float64) BeaconVerdict {
	v := BeaconVerdict{Samples: len(intervals)}
	if len(intervals) < 4 {
		return v
	}
	var sum float64
	for _, x := range intervals {
		sum += x
	}
	mean := sum / float64(len(intervals))
	v.MeanSecs = mean
	if mean <= 0 {
		return v
	}
	var sq float64
	for _, x := range intervals {
		d := x - mean
		sq += d * d
	}
	std := math.Sqrt(sq / float64(len(intervals)))
	v.CV = std / mean

	// Regular cadence (low CV) over a meaningful interval == beaconing.
	if v.CV < 0.25 && mean >= 2 {
		v.Beacon = true
	}
	// Score: lower CV -> higher. Clamp.
	s := (1 - v.CV) * 100
	v.Score = math.Max(0, math.Min(100, s))
	return v
}
