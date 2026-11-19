package ward

import (
	"testing"
	"time"
)

func TestLearnThenFlag(t *testing.T) {
	w := New(20)
	now := time.Unix(1_700_000_000, 0)
	usual := Obs{Host: "echo-dot", Dst: "device-metrics-us.amazon.com", Port: 443, Bytes: 4000, Hour: 19, JA3: "aaa"}
	for i := 0; i < 20; i++ {
		o := usual
		o.Hour = 18 + i%4
		if d := w.Observe(o, now); d != nil {
			t.Fatalf("flagged while learning: %+v", d)
		}
	}
	if p := w.Profiles()[0]; p.Learning || p.Samples != 20 {
		t.Fatalf("profile after learning = %+v", p)
	}
	if d := w.Observe(usual, now); len(d) != 0 {
		t.Errorf("normal behaviour flagged: %+v", d)
	}

	odd := Obs{Host: "echo-dot", Dst: "185.220.101.44", Port: 8443, Bytes: 90000, Hour: 3, JA3: "bbb"}
	kinds := map[string]bool{}
	for _, d := range w.Observe(odd, now) {
		kinds[d.Kind] = true
	}
	for _, want := range []string{"new-destination", "new-port", "odd-hour", "volume", "fingerprint"} {
		if !kinds[want] {
			t.Errorf("missing deviation %q (got %v)", want, kinds)
		}
	}
	if d := w.Observe(odd, now.Add(time.Minute)); len(d) != 0 {
		t.Errorf("the same deviations were reported again within the hour: %d", len(d))
	}
	if len(w.Deviations()) != 5 {
		t.Errorf("recorded %d deviations, want 5", len(w.Deviations()))
	}

	w.Accept(odd)
	if n := len(w.Deviations()); n != 0 {
		t.Errorf("%d deviations left after accepting them", n)
	}
	if d := w.Observe(odd, now.Add(2*time.Hour)); len(d) != 0 {
		t.Errorf("accepted behaviour still flagged: %+v", d)
	}
	w.Relearn("echo-dot")
	if d := w.Observe(odd, now.Add(3*time.Hour)); d != nil || !w.Profiles()[0].Learning {
		t.Error("relearn did not reset the profile")
	}
}

func TestLearningNeedsAFullDay(t *testing.T) {
	w := New(5)
	w.MinAge = 24 * time.Hour
	now := time.Unix(1_700_000_000, 0)
	o := Obs{Host: "cam", Dst: "cloud.example", Port: 443, Hour: 9}
	for i := 0; i < 50; i++ {
		w.Observe(o, now.Add(time.Duration(i)*time.Minute))
	}
	if !w.Profiles()[0].Learning {
		t.Fatal("stopped learning after 50 minutes; it must watch for a day")
	}
	w.Observe(o, now.Add(25*time.Hour))
	if w.Profiles()[0].Learning {
		t.Error("still learning after a day and enough samples")
	}
	saved := w.Export()
	w2 := New(5)
	w2.Import(saved)
	if d := w2.Observe(Obs{Host: "cam", Dst: "elsewhere.example", Port: 443, Hour: 9}, now.Add(26*time.Hour)); len(d) != 1 || d[0].Kind != "new-destination" {
		t.Errorf("restored profile did not flag a new destination: %+v", d)
	}
}
