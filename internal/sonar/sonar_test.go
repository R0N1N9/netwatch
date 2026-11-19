package sonar

import (
	"testing"
	"time"
)

func state(e *Engine, now time.Time) NodeState { return e.Snapshot(now)[0] }

func TestMotionDetection(t *testing.T) {
	e := New()
	var fired int
	e.OnMotion = func(string, float64) { fired++ }
	sim := NewSim("node-A", 1)
	now := time.Unix(1_700_000_000, 0)
	feed := func(n int, motion float64) {
		for i := 0; i < n; i++ {
			now = now.Add(40 * time.Millisecond)
			e.Ingest(sim.Frame(motion), now)
		}
	}

	feed(30, 0)
	if s := state(e, now); s.State != "calibrating" {
		t.Fatalf("early state = %q, want calibrating", s.State)
	}
	feed(150, 0) // finish calibration in a still room
	if s := state(e, now); s.State != "clear" || s.Score > 25 {
		t.Fatalf("still room: state=%q score=%.1f, want clear/low", s.State, s.Score)
	}
	feed(60, 1) // someone walks the link
	if s := state(e, now); s.State != "motion" || s.Score < 50 {
		t.Fatalf("motion: state=%q score=%.1f, want motion/high", s.State, s.Score)
	}
	if fired != 1 {
		t.Errorf("OnMotion fired %d times, want 1", fired)
	}
	feed(120, 0) // room goes still again
	if s := state(e, now); s.State != "clear" {
		t.Fatalf("after motion: state=%q, want clear", s.State)
	}
	if s := state(e, now.Add(10*time.Second)); s.State != "offline" {
		t.Errorf("stale node state=%q, want offline", s.State)
	}
}

func TestIngestRejectsBadFrames(t *testing.T) {
	e := New()
	now := time.Now()
	e.Ingest(Frame{Node: "", Amp: make([]float64, 52)}, now)
	e.Ingest(Frame{Node: "x", Amp: []float64{1, 2}}, now)
	e.Ingest(Frame{Node: "x", Amp: make([]float64, 52)}, now) // all zero
	if n := len(e.Snapshot(now)); n != 0 {
		t.Errorf("bad frames created %d nodes, want 0", n)
	}
}
