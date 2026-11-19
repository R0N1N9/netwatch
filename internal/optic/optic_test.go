package optic

import (
	"math"
	"testing"
	"time"
)

func TestInView(t *testing.T) {
	things := []Thing{
		{Kind: "ap", Label: "AP", X: 0, Y: -1},      // straight up the map from the origin
		{Kind: "node", Label: "east", X: 1, Y: 0},   // to the right
		{Kind: "host", Label: "south", X: 0, Y: 1},  // behind
		{Kind: "host", Label: "far", X: 0, Y: -1.9}, // ahead, further than the AP
		{Kind: "host", Label: "here", X: 0, Y: 0},   // on top of the phone
	}
	got := InView(0, 0, 0, things) // facing up the map
	if len(got) != 2 || got[0].Label != "AP" || got[1].Label != "far" {
		t.Fatalf("facing up: got %+v, want the AP then the far host", got)
	}
	if math.Abs(got[0].Dist-4.4) > 0.05 || got[0].Rel != 0 {
		t.Fatalf("AP should be 4.4 m dead ahead, got %+v", got[0])
	}
	got = InView(0, 0, 90, things) // facing right
	if len(got) != 1 || got[0].Label != "east" {
		t.Fatalf("facing right: got %+v, want only the east node", got)
	}
	got = InView(0, 0, 70, things) // the east node is 20 degrees to the right of centre
	if len(got) != 1 || got[0].Rel != 20 {
		t.Fatalf("facing 70: got %+v, want east at +20", got)
	}
	if got := InView(0, 0, 340, []Thing{{X: 0, Y: -1}}); len(got) != 1 || got[0].Rel != 20 {
		t.Fatalf("wrap-around: got %+v, want +20", got)
	}
	if got := InView(-2, -2, 135, []Thing{{X: 2, Y: 2}}); len(got) != 0 {
		t.Fatalf("out of reach should not be seen, got %+v", got)
	}
}

func TestAddCleansAndLimits(t *testing.T) {
	r := New()
	now := time.Unix(1_700_000_000, 0)
	things := []Thing{{Kind: "ap", Label: "AP", X: 0, Y: -1}}
	if !r.Add("10.0.0.5", "pixel", Frame{X: 99, Y: math.NaN(), Heading: -90, Pitch: 400, Mode: "<script>", Target: "aa:bb:cc:dd:ee:ff"}, things, now) {
		t.Fatal("first frame refused")
	}
	f := r.Trail("10.0.0.5", 0)[0]
	if f.X != 2 || f.Y != 0 || f.Heading != 270 || f.Pitch != 90 || f.Mode != "alerts" || f.T != now.UnixMilli() {
		t.Fatalf("frame not cleaned: %+v", f)
	}
	if r.Add("10.0.0.5", "pixel", Frame{}, things, now.Add(20*time.Millisecond)) {
		t.Fatal("a frame 20 ms after the last should be dropped")
	}
	if !r.Add("10.0.0.5", "pixel", Frame{Mode: "find"}, things, now.Add(300*time.Millisecond)) {
		t.Fatal("a frame 300 ms later should be kept")
	}
	if got := r.Trail("10.0.0.5", 0)[1]; len(got.Seen) != 1 || got.Seen[0].Label != "AP" {
		t.Fatalf("what was in view should be worked out on arrival: %+v", got)
	}
	if got := r.Trail("10.0.0.5", now.UnixMilli()); len(got) != 1 {
		t.Fatalf("since should return only newer frames, got %d", len(got))
	}
	for i := 0; i < maxFrames+50; i++ {
		r.Add("10.0.0.5", "pixel", Frame{}, nil, now.Add(time.Duration(i+2)*time.Second))
	}
	if n := len(r.Trail("10.0.0.5", 0)); n != maxFrames {
		t.Fatalf("trail should be capped at %d, got %d", maxFrames, n)
	}
}

func TestPhonesAndForget(t *testing.T) {
	r := New()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < maxPhones; i++ {
		if !r.Add(string(rune('a'+i)), "", Frame{}, nil, now) {
			t.Fatalf("phone %d refused", i)
		}
	}
	if r.Add("one-too-many", "", Frame{}, nil, now) {
		t.Fatal("more phones than the cap should be refused")
	}
	r.Add("a", "first", Frame{}, nil, now.Add(time.Second))
	ps := r.Phones(now.Add(2 * time.Second))
	if len(ps) != maxPhones || ps[0].Phone != "a" || ps[0].Name != "first" || !ps[0].Live || ps[0].Frames != 2 {
		t.Fatalf("unexpected listing: %+v", ps[0])
	}
	if ps := r.Phones(now.Add(time.Minute)); ps[0].Live {
		t.Fatal("a phone silent for a minute is not live")
	}
	r.Forget("a")
	if len(r.Phones(now)) != maxPhones-1 || len(r.Trail("a", 0)) != 0 {
		t.Fatal("forget did not drop the trail")
	}
	r.Forget("*")
	if len(r.Phones(now)) != 0 {
		t.Fatal("forget * should drop everything")
	}
	// a phone that went quiet long ago makes room for a new one
	for i := 0; i < maxPhones; i++ {
		r.Add(string(rune('a'+i)), "", Frame{}, nil, now)
	}
	if !r.Add("late", "", Frame{}, nil, now.Add(keepFor+time.Minute)) {
		t.Fatal("old trails should be dropped to make room")
	}
}
