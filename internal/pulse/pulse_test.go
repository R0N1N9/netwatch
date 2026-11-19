package pulse

import (
	"math/rand"
	"testing"
)

// typist generates strokes with a given rhythm.
func typist(rng *rand.Rand, n int, dwell, dwellSD, flight, flightSD float64) []Stroke {
	out := make([]Stroke, n)
	for i := range out {
		out[i] = Stroke{Dwell: dwell + rng.NormFloat64()*dwellSD, Flight: flight + rng.NormFloat64()*flightSD}
	}
	return out
}

func TestMatchOwnerAndStranger(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	owner := func(n int) []Stroke { return typist(rng, n, 95, 18, 150, 45) }
	stranger := func(n int) []Stroke { return typist(rng, n, 62, 10, 95, 25) }

	p, ok := Enroll(owner(200))
	if !ok {
		t.Fatal("enrolment failed")
	}
	if _, ok := Enroll(owner(40)); ok {
		t.Error("enrolled from too little typing")
	}
	var own, other float64
	for i := 0; i < 20; i++ {
		own += Match(p, owner(WindowN))
		other += Match(p, stranger(WindowN))
	}
	if own/20 < 70 || other/20 > 20 {
		t.Errorf("average match: owner %.0f (want > 70), stranger %.0f (want < 20)", own/20, other/20)
	}
}

func TestOwnerOnADifferentDayStillMatches(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	p, _ := Enroll(typist(rng, 200, 95, 18, 150, 45))
	var later float64
	for i := 0; i < 20; i++ {
		later += Match(p, typist(rng, WindowN, 99, 19, 144, 43)) // 4% slower on the keys, 4% quicker between them
	}
	if later/20 < 60 {
		t.Errorf("the owner, slightly off their usual rhythm, scored %.0f (want > 60)", later/20)
	}
	// A very even typist has almost no spread; a few milliseconds of drift must not read as a stranger.
	even, _ := Enroll(typist(rng, 200, 70, 4, 60, 5))
	if m := Match(even, typist(rng, WindowN, 73, 4, 63, 5)); m < 60 {
		t.Errorf("an even typist 3 ms off scored %.0f (want > 60)", m)
	}
}

func TestMonitorLifecycle(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	var m Monitor
	fired := 0
	m.OnMismatch = func(float64) { fired++ }

	if st := m.Feed(typist(rng, 60, 95, 18, 150, 45)); st.Enrolled || st.Progress != 60 {
		t.Fatalf("mid-enrolment state = %+v", st)
	}
	if st := m.Feed(typist(rng, 120, 95, 18, 150, 45)); !st.Enrolled {
		t.Fatalf("not enrolled after enough typing: %+v", st)
	}
	if st := m.Feed(typist(rng, 200, 95, 18, 150, 45)); st.Mismatch || fired != 0 {
		t.Errorf("the owner was rejected: %+v fired=%d", st, fired)
	}
	// 150 strokes: the first window still holds some of the owner's typing, the next two are all the stranger's
	if st := m.Feed(typist(rng, 150, 62, 10, 95, 25)); !st.Mismatch || fired != 1 {
		t.Errorf("a stranger was not flagged after two windows: %+v fired=%d", st, fired)
	}
	m.Reset()
	if st := m.State(); st.Enrolled || st.Progress != 0 {
		t.Errorf("reset state = %+v", st)
	}
}

func TestOnePauseDoesNotSpoilTheProfile(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	sample := typist(rng, 200, 70, 5, 60, 6)
	for i := 20; i < 200; i += 40 { // a handful of stalls and long holds while enrolling
		sample[i] = Stroke{Dwell: 380, Flight: 650}
	}
	p, ok := Enroll(sample)
	if !ok {
		t.Fatal("enrolment failed")
	}
	if m := Match(p, typist(rng, WindowN, 70, 5, 60, 6)); m < 70 {
		t.Errorf("the same typist scored %.0f after an enrolment with a few stalls (want > 70)", m)
	}
}
