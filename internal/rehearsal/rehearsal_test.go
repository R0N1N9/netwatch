package rehearsal

import (
	"testing"
	"time"
)

func TestGhostCleanRunAndSlowRun(t *testing.T) {
	sc, ok := Find("ghost")
	if !ok {
		t.Fatal("no ghost scenario")
	}
	t0 := time.Unix(1_700_000_000, 0)
	s := Start(sc, "192.168.1.250", t0)
	if d := s.Due(t0.Add(time.Second)); len(d) != 0 {
		t.Fatalf("injected %d steps before their time", len(d))
	}
	if ok, _ := s.Act("investigate", t0.Add(time.Second)); ok {
		t.Error("credited a response before its alert existed")
	}
	// prompt answers to each step as it arrives
	for _, at := range []struct {
		sec int
		act string
	}{{2, "investigate"}, {14, "reach"}, {28, "quarantine"}, {36, "note"}} {
		now := t0.Add(time.Duration(at.sec) * time.Second)
		s.Due(now)
		if ok, _ := s.Act(at.act, now.Add(5*time.Second)); !ok {
			t.Errorf("%s at %ds was not credited", at.act, at.sec)
		}
	}
	if !s.Tick(t0.Add(42*time.Second)) || s.Score() != 100 || s.State().Verdict != "clean run" {
		t.Errorf("clean run: score %d verdict %q", s.Score(), s.State().Verdict)
	}

	slow := Start(sc, "192.168.1.250", t0)
	slow.Due(t0.Add(40 * time.Second)) // everything injected
	slow.Act("investigate", t0.Add(3*time.Minute))
	if slow.Tick(t0.Add(100 * time.Second)) {
		t.Error("ended while there was still time")
	}
	if !slow.Tick(t0.Add(130*time.Second)) || slow.Score() != 10 {
		t.Errorf("slow run: score %d, want 10 (one late answer at 40%% of 25)", slow.Score())
	}
}

func TestForbiddenActionCostsPoints(t *testing.T) {
	sc, _ := Find("mirror")
	t0 := time.Unix(1_700_000_000, 0)
	s := Start(sc, "192.168.1.1", t0)
	s.Due(t0.Add(30 * time.Second))
	s.Act("investigate", t0.Add(31*time.Second))
	if ok, pen := s.Act("quarantine", t0.Add(32*time.Second)); ok || pen == "" {
		t.Error("quarantining the gateway was not penalised")
	}
	if s.Score() != 33-15 || len(s.State().Penalties) != 1 {
		t.Errorf("score %d penalties %v", s.Score(), s.State().Penalties)
	}
}
