// Package optic keeps a short memory of what a paired phone was looking at:
// where it stood on the room map, which way it faced, and which of the things
// SONAR knows about were in its field of view. The console's BRAINDANCE deck
// plays this back.
//
// Only numbers come from the phone (position, heading, tilt, the tab that was
// open and the device it was looking for). No camera picture or sound is ever
// sent. Trails live in memory only and are capped at about ten minutes a phone.
package optic

import (
	"math"
	"sort"
	"sync"
	"time"
)

const (
	MetresPerUnit = 4.4 // the room map's scale, the same one SONAR uses
	FOV           = 62  // the horizontal field of view the phone's X-RAY draws with, in degrees
	Reach         = 12  // metres: things further than this are not counted as seen

	maxFrames = 2400 // ten minutes at four frames a second
	maxPhones = 8
	maxSeen   = 12
	minGap    = 100 * time.Millisecond
	liveFor   = 5 * time.Second
	keepFor   = 6 * time.Hour
)

// Thing is something on the room map a phone could be looking at.
type Thing struct {
	Kind  string  `json:"k"` // ap | node | host | presence
	Label string  `json:"l"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// Seen is a Thing that was in view, with how far away it was and how far off
// the centre of the view (degrees, negative is left).
type Seen struct {
	Thing
	Dist float64 `json:"d"`
	Rel  float64 `json:"r"`
}

// Frame is one moment of a phone's view. T is unix milliseconds.
type Frame struct {
	T       int64   `json:"t"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Heading float64 `json:"h"`           // degrees clockwise from "up the map"
	Pitch   float64 `json:"p"`           // degrees, up is positive
	Mode    string  `json:"m"`           // the tab that was open: alerts | find | xray | dance
	Target  string  `json:"g,omitempty"` // the device being looked for in FIND
	Seen    []Seen  `json:"s,omitempty"`
	// Presence is where SONAR placed movement in the room at that moment, if
	// anything was moving. Like Seen it is filled in by the console, never by the phone.
	Presence *[2]float64 `json:"e,omitempty"`
}

// Summary describes one phone's trail.
type Summary struct {
	Phone  string `json:"phone"`
	Name   string `json:"name"`
	Frames int    `json:"frames"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Live   bool   `json:"live"` // a frame arrived in the last few seconds
}

type trail struct {
	name   string
	frames []Frame
	last   time.Time
}

// Recorder holds every phone's trail.
type Recorder struct {
	mu     sync.Mutex
	phones map[string]*trail
}

func New() *Recorder { return &Recorder{phones: map[string]*trail{}} }

var modes = map[string]bool{"alerts": true, "find": true, "xray": true, "dance": true}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Max(lo, math.Min(hi, v))
}

// InView works out which things a pose can see: inside the field of view and
// within reach, nearest first.
func InView(x, y, heading float64, things []Thing) []Seen {
	out := []Seen{}
	for _, t := range things {
		dx, dy := (t.X-x)*MetresPerUnit, (t.Y-y)*MetresPerUnit
		dist := math.Hypot(dx, dy)
		if dist < 0.05 || dist > Reach {
			continue
		}
		rel := math.Mod(math.Atan2(dx, -dy)*180/math.Pi-heading+540, 360) - 180
		if math.Abs(rel) >= FOV/2 {
			continue
		}
		out = append(out, Seen{Thing: t, Dist: math.Round(dist*10) / 10, Rel: math.Round(rel)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dist < out[j].Dist })
	if len(out) > maxSeen {
		out = out[:maxSeen]
	}
	return out
}

// Add records one frame from a phone. The frame is cleaned first: values are
// clamped to the map, an unknown mode becomes "alerts", and what was in view
// is worked out here from things rather than taken from the phone. It reports
// false when the frame was dropped (too soon after the last one, or too many
// phones).
func (r *Recorder) Add(phone, name string, f Frame, things []Thing, now time.Time) bool {
	if phone == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, t := range r.phones { // forget phones that went quiet long ago
		if now.Sub(t.last) > keepFor {
			delete(r.phones, id)
		}
	}
	t := r.phones[phone]
	if t == nil {
		if len(r.phones) >= maxPhones {
			return false
		}
		t = &trail{}
		r.phones[phone] = t
	} else if now.Sub(t.last) < minGap {
		return false
	}
	if !modes[f.Mode] {
		f.Mode = "alerts"
	}
	f.T = now.UnixMilli()
	f.X, f.Y = clamp(f.X, -2, 2), clamp(f.Y, -2, 2)
	f.Heading = math.Mod(clamp(f.Heading, -720, 720)+720, 360)
	f.Pitch = clamp(f.Pitch, -90, 90)
	if len(f.Target) > 40 {
		f.Target = ""
	}
	f.Seen = InView(f.X, f.Y, f.Heading, things)
	f.Presence = nil
	for _, th := range things {
		if th.Kind == "presence" {
			f.Presence = &[2]float64{th.X, th.Y}
		}
	}
	t.name, t.last = name, now
	t.frames = append(t.frames, f)
	if len(t.frames) > maxFrames {
		t.frames = append([]Frame(nil), t.frames[len(t.frames)-maxFrames:]...)
	}
	return true
}

// Phones lists the trails, the most recently active first.
func (r *Recorder) Phones(now time.Time) []Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Summary{}
	for id, t := range r.phones {
		if len(t.frames) == 0 {
			continue
		}
		out = append(out, Summary{Phone: id, Name: t.name, Frames: len(t.frames), Start: t.frames[0].T, End: t.frames[len(t.frames)-1].T, Live: now.Sub(t.last) < liveFor})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].End > out[j].End || (out[i].End == out[j].End && out[i].Phone < out[j].Phone)
	})
	return out
}

// Trail returns a phone's frames from since (unix ms) onwards, oldest first.
func (r *Recorder) Trail(phone string, since int64) []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.phones[phone]
	if t == nil {
		return []Frame{}
	}
	i := sort.Search(len(t.frames), func(i int) bool { return t.frames[i].T > since })
	return append([]Frame{}, t.frames[i:]...)
}

// Forget drops one phone's trail, or every trail when phone is "*".
func (r *Recorder) Forget(phone string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if phone == "*" {
		r.phones = map[string]*trail{}
		return
	}
	delete(r.phones, phone)
}
