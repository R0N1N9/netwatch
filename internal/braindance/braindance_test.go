package braindance

import (
	"testing"
	"time"
)

func fill(r *Recorder, start time.Time, n int) {
	for i := 0; i < n; i++ {
		score := 0.0
		if i > n/3 && i < 2*n/3 {
			score = 80
		}
		r.Add(Sample{T: start.Add(time.Duration(i) * 200 * time.Millisecond).UnixMilli(),
			Nodes: []NodeSample{{Node: "node-A", State: "clear", Score: score}}})
	}
}

func TestSaveListLoadPersistDelete(t *testing.T) {
	dir := t.TempDir()
	start := time.Unix(1_700_000_000, 0)
	r := New(dir)
	fill(r, start, 100) // 20 s
	if s := r.BufferSeconds(); s < 19 || s > 21 {
		t.Fatalf("buffer = %.1fs, want ~20", s)
	}
	layout := map[string]Point{"node-A": {X: 0.5, Y: -0.2}}
	c, err := r.Save("kitchen walk", start.Add(5*time.Second).UnixMilli(), start.Add(15*time.Second).UnixMilli(), layout, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Peak != 80 || c.Name != "kitchen walk" || (c.End-c.Start) < 9000 {
		t.Fatalf("clip = %+v", c)
	}
	if _, err := r.Save("", 1, 2, nil, false); err == nil {
		t.Error("saving an empty span should fail")
	}

	// a fresh recorder on the same directory sees the clip
	r2 := New(dir)
	list := r2.List()
	if len(list) != 1 || list[0].ID != c.ID || len(list[0].Samples) != 0 {
		t.Fatalf("reloaded list = %+v", list)
	}
	full, ok := r2.Load(c.ID)
	if !ok || len(full.Samples) < 45 || full.Layout["node-A"].X != 0.5 {
		t.Fatalf("loaded clip: ok=%v samples=%d layout=%v", ok, len(full.Samples), full.Layout)
	}
	if _, ok := r2.Load("../../etc/passwd"); ok {
		t.Error("path-like id accepted")
	}
	if !r2.Delete(c.ID) || len(New(dir).List()) != 0 {
		t.Error("delete did not remove the clip from disk")
	}
}

func TestManualRecordingInMemory(t *testing.T) {
	r := New("")
	start := time.Unix(1_700_000_000, 0)
	fill(r, start, 20)
	r.StartRec(start.Add(4 * time.Second))
	if r.RecordingSince() == 0 {
		t.Fatal("not recording after StartRec")
	}
	fill(r, start.Add(4*time.Second), 30)
	c, err := r.StopRec("", start.Add(10*time.Second), nil)
	if err != nil {
		t.Fatal(err)
	}
	full, ok := r.Load(c.ID)
	if !ok || len(full.Samples) < 25 || r.RecordingSince() != 0 || full.Name == "" {
		t.Fatalf("ok=%v samples=%d name=%q", ok, len(full.Samples), full.Name)
	}
	if _, err := r.StopRec("", start, nil); err == nil {
		t.Error("StopRec without StartRec should fail")
	}
}
