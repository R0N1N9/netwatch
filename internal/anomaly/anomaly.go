// Package anomaly is an online behavioral-anomaly model: it keeps a streaming
// mean/variance baseline per (device, feature) and scores new observations by
// how many standard deviations they deviate. A lightweight, explainable stand-in
// for a full isolation forest — upgradeable behind the same API.
package anomaly

import (
	"math"
	"sort"
	"sync"
)

type stat struct {
	n        int
	mean, m2 float64 // Welford's online variance
}

func (s *stat) add(x float64) {
	s.n++
	d := x - s.mean
	s.mean += d / float64(s.n)
	s.m2 += d * (x - s.mean)
}

func (s *stat) std() float64 {
	if s.n < 2 {
		return 0
	}
	return math.Sqrt(s.m2 / float64(s.n-1))
}

func (s *stat) z(x float64) float64 {
	sd := s.std()
	if sd == 0 {
		return 0
	}
	return (x - s.mean) / sd
}

// Model holds per-device feature baselines.
type Model struct {
	mu    sync.Mutex
	feats map[string]map[string]*stat
	minN  int
}

// New returns an empty model.
func New() *Model {
	return &Model{feats: map[string]map[string]*stat{}, minN: 6}
}

// Score returns an anomaly score (0-100) for dev's features against the current
// baseline, plus the feature that contributed most — without updating state.
func (m *Model) Score(dev string, f map[string]float64) (float64, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fm := m.feats[dev]
	if fm == nil {
		return 0, ""
	}
	var sum float64
	var cnt int
	topZ, topK := 0.0, ""
	for k, v := range f {
		s := fm[k]
		if s == nil || s.n < m.minN {
			continue
		}
		z := math.Abs(s.z(v))
		sum += z
		cnt++
		if z > topZ {
			topZ, topK = z, k
		}
	}
	if cnt == 0 {
		return 0, ""
	}
	return math.Min(100, (sum/float64(cnt))*22), topK
}

// Observe folds an observation into the baselines.
func (m *Model) Observe(dev string, f map[string]float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fm := m.feats[dev]
	if fm == nil {
		fm = map[string]*stat{}
		m.feats[dev] = fm
	}
	for k, v := range f {
		s := fm[k]
		if s == nil {
			s = &stat{}
			fm[k] = s
		}
		s.add(v)
	}
}

// Devices returns the device keys with baselines (sorted), for introspection.
func (m *Model) Devices() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.feats))
	for k := range m.feats {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
