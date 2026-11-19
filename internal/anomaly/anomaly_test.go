package anomaly

import "testing"

func TestModelScore(t *testing.T) {
	m := New()
	dev := "192.168.1.50"
	// establish a realistic baseline (values vary around ~1000 bps, ~3 dsts)
	bps := []float64{900, 1100, 950, 1050, 1000, 980, 1020, 1010, 990, 1005}
	dsts := []float64{2, 3, 4, 3, 3, 2, 4, 3, 3, 3}
	for i := range bps {
		m.Observe(dev, map[string]float64{"bps": bps[i], "dsts": dsts[i]})
	}
	// in-distribution => low score
	if s, _ := m.Score(dev, map[string]float64{"bps": 1020, "dsts": 3}); s > 15 {
		t.Errorf("in-distribution score = %.1f, want low", s)
	}
	// large deviation => high score, blamed on the right feature
	s, top := m.Score(dev, map[string]float64{"bps": 50000, "dsts": 3})
	if s < 40 {
		t.Errorf("outlier score = %.1f, want high", s)
	}
	if top != "bps" {
		t.Errorf("top feature = %q, want bps", top)
	}
	// unknown device => 0
	if s, _ := m.Score("10.0.0.9", map[string]float64{"bps": 1}); s != 0 {
		t.Errorf("unknown device score = %.1f, want 0", s)
	}
}
