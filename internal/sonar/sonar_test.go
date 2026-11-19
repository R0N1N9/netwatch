package sonar

import (
	"fmt"
	"math"
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

func TestBreathingAndRange(t *testing.T) {
	e := New()
	still, breather := NewSim("empty", 1), NewSim("occupied", 2)
	breather.BreathBPM, breather.DistM = 15, 3.2
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 25*40; i++ { // 40 s at 25 fps, nobody moving
		now = now.Add(40 * time.Millisecond)
		e.Ingest(still.Frame(0), now)
		e.Ingest(breather.Frame(0), now)
	}
	got := map[string]NodeState{}
	for _, s := range e.Snapshot(now) {
		got[s.Node] = s
	}
	if b := got["occupied"].Breath; b < 13 || b > 17 {
		t.Errorf("occupied link breath = %.0f/min (conf %.1f), want ~15", b, got["occupied"].BreathConf)
	}
	if b := got["empty"].Breath; b != 0 {
		t.Errorf("empty link breath = %.0f/min (conf %.1f), want none", b, got["empty"].BreathConf)
	}
	if d := got["occupied"].DistM; d < 3.0 || d > 3.4 {
		t.Errorf("range = %.2f m, want ~3.2", d)
	}
	if d := got["empty"].DistM; d != 0 {
		t.Errorf("unranged node distance = %.2f, want 0", d)
	}
}

func TestActivityAndFall(t *testing.T) {
	e := New()
	var falls int
	e.OnFall = func(string) { falls++ }
	sim := NewSim("hall", 4)
	now := time.Unix(1_700_000_000, 0)
	feed := func(n int, motion float64) {
		for i := 0; i < n; i++ {
			now = now.Add(40 * time.Millisecond)
			e.Ingest(sim.Frame(motion), now)
		}
	}
	act := func() string { return e.Snapshot(now)[0].Activity }

	feed(200, 0)
	if a := act(); a != "empty" {
		t.Fatalf("still room activity = %q, want empty", a)
	}
	feed(100, 1) // four seconds of movement
	if a := act(); a != "walking" {
		t.Errorf("sustained movement = %q, want walking", a)
	}
	feed(400, 0) // settle for a while; a long walk is not a fall
	if falls != 0 {
		t.Errorf("a walk was flagged as a fall")
	}
	feed(20, 1)  // under a second of violent movement...
	feed(150, 0) // ...then nothing
	if falls != 1 {
		t.Errorf("fall-like burst flagged %d times, want 1", falls)
	}
}

func TestEstimatePeopleAndTripwire(t *testing.T) {
	layout := map[string]Pt{APKey: {0, 0}, "east": {1, 0}, "east2": {1, 0.2}, "north": {0, -1}}
	nodes := []NodeState{
		{Node: "east", State: "motion", Score: 80},
		{Node: "east2", State: "motion", Score: 40},
		{Node: "north", State: "clear", Breath: 14},
	}
	p, ok := Estimate(nodes, layout)
	if !ok || p.X < 0.45 || p.X > 0.55 || p.Y < 0 || p.Y > 0.1 {
		t.Errorf("estimate = %+v ok=%v, want roughly (0.5, 0.03)", p, ok)
	}
	if n := People(nodes, layout); n != 2 { // two links east = one person, plus the still breather north
		t.Errorf("people = %d, want 2", n)
	}
	ranged, _ := NodePos(NodeState{Node: "east", DistM: 2.2}, layout)
	if math.Abs(ranged.X-0.5) > 1e-9 || ranged.Y != 0 {
		t.Errorf("ranged node at %+v, want (0.5, 0)", ranged)
	}
	wire := Wire{A: Pt{0.3, -1}, B: Pt{0.3, 1}}
	if !Crossed(Pt{0.1, 0}, Pt{0.6, 0.1}, wire) || Crossed(Pt{0.4, 0}, Pt{0.6, 0.1}, wire) {
		t.Error("tripwire crossing test is wrong")
	}
}

func TestLocateRadio(t *testing.T) {
	e := New()
	now := time.Unix(1_700_000_000, 0)
	layout := map[string]Pt{APKey: {0, 0}, "a": {-1, -1}, "b": {1, -1}, "c": {0, 1}}
	truth := Pt{0.4, -0.2}
	var nodes []NodeState
	for _, name := range []string{"a", "b", "c"} {
		nodes = append(nodes, NodeState{Node: name})
		d := math.Hypot(truth.X-layout[name].X, truth.Y-layout[name].Y) * MetresPerUnit
		rssi := rssiAt1m - 10*pathLossExp*math.Log10(d)
		e.Ingest(Frame{Node: name, Seen: []Sighting{{MAC: "aa:bb:cc:dd:ee:01", RSSI: int(math.Round(rssi))}}}, now)
	}
	r := e.Radios(now, nodes, layout)
	if len(r) != 1 || r[0].Nodes != 3 {
		t.Fatalf("radios = %+v", r)
	}
	if err := math.Hypot(r[0].X-truth.X, r[0].Y-truth.Y) * MetresPerUnit; err > 0.6 {
		t.Errorf("located %.2f m from the truth (%s)", err, fmt.Sprintf("%+v", r[0]))
	}
	if got := e.Radios(now.Add(2*time.Minute), nodes, layout); len(got) != 0 {
		t.Errorf("stale radio still reported: %+v", got)
	}
}
