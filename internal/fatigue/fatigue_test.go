package fatigue

import "testing"

func TestAssess(t *testing.T) {
	if r := Assess(Input{SessionMin: 30, Hour: 14}); r.Sharpness != 100 || r.Level != "sharp" || len(r.Factors) != 0 {
		t.Errorf("fresh operator = %+v", r)
	}
	// three hours in, late, typing 20% slower and uneven
	r := Assess(Input{SessionMin: 180, Hour: 23, HaveRhythm: true, HoldDrift: 0.22, GapDrift: 0.18, SpreadRatio: 1.6})
	if r.Level != "tiring" || len(r.Factors) != 4 {
		t.Errorf("long evening = %+v", r)
	}
	// 3 a.m., five hours, alerts ignored
	r = Assess(Input{SessionMin: 300, Hour: 3, HaveRhythm: true, HoldDrift: 0.3, GapDrift: 0.3, SpreadRatio: 2, Unopened: 4, OldestMin: 40})
	if r.Level != "spent" || r.Sharpness > 20 {
		t.Errorf("3 a.m. = %+v", r)
	}
	// rhythm numbers are ignored without a profile
	if r := Assess(Input{Hour: 12, HoldDrift: 0.5, GapDrift: 0.5, SpreadRatio: 3}); r.Sharpness != 100 {
		t.Errorf("no profile, yet rhythm counted: %+v", r)
	}
}
