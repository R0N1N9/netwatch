package recap

import (
	"strings"
	"testing"
	"time"
)

func TestTrackerAndRecap(t *testing.T) {
	tr := Tracker{AwayAfter: 10 * time.Minute}
	t0 := time.Unix(1_700_000_000, 0)
	if tr.Feed(true, t0) != nil {
		t.Fatal("a recap on first sight")
	}
	if tr.Feed(false, t0.Add(4*time.Minute)) != nil || tr.Away() {
		t.Error("four minutes of nothing counted as away")
	}
	if tr.Feed(true, t0.Add(5*time.Minute)) != nil {
		t.Error("a recap after a five-minute gap")
	}
	tr.Feed(false, t0.Add(16*time.Minute))
	if !tr.Away() {
		t.Fatal("eleven minutes of nothing did not count as away")
	}
	span := tr.Feed(true, t0.Add(52*time.Minute))
	if span == nil || !span.From.Equal(t0.Add(5*time.Minute)) || tr.Away() {
		t.Fatalf("span = %+v", span)
	}
	ms := func(min int) int64 { return t0.Add(time.Duration(min) * time.Minute).UnixMilli() }
	r := Build(*span, []Event{{ms(2), "CRITICAL", "x", "before"}, {ms(30), "CRITICAL", "decoy", "touched"}, {ms(20), "WARN", "ward", "new port"}},
		[]string{"a", "b"}, []string{"b", "c"}, 3)
	if r.Critical != 1 || r.Warnings != 1 || len(r.Events) != 2 || r.Events[0].Kind != "ward" || r.Joined[0] != "c" || r.Left[0] != "a" {
		t.Errorf("recap = %+v", r)
	}
	for _, want := range []string{"47 minutes", "1 critical alert", "1 new device", "movement sensed 3 times"} {
		if !strings.Contains(r.Headline, want) {
			t.Errorf("headline %q lacks %q", r.Headline, want)
		}
	}
	if q := Build(*span, nil, []string{"a"}, []string{"a"}, 0); !strings.HasPrefix(q.Headline, "Quiet.") {
		t.Errorf("quiet headline = %q", q.Headline)
	}
}
