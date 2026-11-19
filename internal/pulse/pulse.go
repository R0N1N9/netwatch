// Package pulse recognises a person by the rhythm of their typing. It works
// only on timings — how long each key is held and the gap to the next — and
// never sees or stores which keys were pressed.
package pulse

import (
	"math"
	"sort"
	"sync"
)

// Stroke is the timing of one key press, in milliseconds.
type Stroke struct {
	Dwell  float64 `json:"dwell"`  // how long the key was held
	Flight float64 `json:"flight"` // gap from the previous key's release to this press
}

func (s Stroke) valid() bool {
	return s.Dwell > 5 && s.Dwell < 600 && s.Flight > -200 && s.Flight < 1500
}

// dist is a robust summary: the median and the spread around it (the median
// absolute deviation, scaled to read like a standard deviation). One long
// pause or a stalled key does not move either.
type dist struct{ Mean, Std float64 }

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	} else {
		return (s[n/2-1] + s[n/2]) / 2
	}
}

func summarise(v []float64) dist {
	if len(v) == 0 {
		return dist{}
	}
	m := median(v)
	dev := make([]float64, len(v))
	for i, x := range v {
		dev[i] = math.Abs(x - m)
	}
	return dist{m, 1.4826 * median(dev)}
}

// Profile is someone's typing rhythm.
type Profile struct {
	N      int  `json:"n"`
	Dwell  dist `json:"dwell"`
	Flight dist `json:"flight"`
}

const (
	EnrollN = 150 // strokes needed to build a profile
	WindowN = 50  // strokes compared at a time
)

func split(strokes []Stroke) (dwell, flight []float64) {
	for _, s := range strokes {
		if s.valid() {
			dwell = append(dwell, s.Dwell)
			if s.Flight < 700 { // longer gaps are pauses to think, not rhythm
				flight = append(flight, s.Flight)
			}
		}
	}
	return
}

// Enroll builds a profile from a typing sample.
func Enroll(strokes []Stroke) (Profile, bool) {
	d, f := split(strokes)
	if len(d) < EnrollN || len(f) < EnrollN/2 {
		return Profile{}, false
	}
	return Profile{N: len(d), Dwell: summarise(d), Flight: summarise(f)}, true
}

// Match scores a window of typing against a profile, 0 (someone else) to 100
// (the same rhythm).
func Match(p Profile, window []Stroke) float64 {
	d, f := split(window)
	if len(d) < 10 || len(f) < 5 {
		return 100 // not enough to judge
	}
	wd, wf := summarise(d), summarise(f)
	// How far each window average sits from the profile, in units of the
	// spread you would expect: sampling noise for a window this size, plus
	// the few percent anyone's rhythm drifts between sittings and texts.
	z := func(w, ref dist, n int) float64 {
		sampling := ref.Std * ref.Std / float64(n)
		drift := 0.05 * ref.Mean
		return math.Abs(w.Mean-ref.Mean) / math.Sqrt(sampling+drift*drift+4)
	}
	spread := func(w, ref dist) float64 {
		return math.Abs(math.Log(math.Max(w.Std, 1) / math.Max(ref.Std, 1)))
	}
	dist := (z(wd, p.Dwell, len(d))+z(wf, p.Flight, len(f)))/2 + 2*(spread(wd, p.Dwell)+spread(wf, p.Flight))
	return 100 * math.Exp(-dist*dist/30)
}

// Monitor enrolls, then scores typing continuously.
type Monitor struct {
	mu      sync.Mutex
	enroll  []Stroke
	profile *Profile
	window  []Stroke
	last    float64
	lastWin [2]dist // hold and gap summary of the most recent full window
	haveWin bool
	low     int // consecutive low-scoring windows

	// OnMismatch fires after two consecutive windows that do not look like the owner.
	OnMismatch func(score float64)
}

// State is what the console shows.
type State struct {
	Enrolled bool    `json:"enrolled"`
	Progress int     `json:"progress"` // strokes collected toward enrolment
	Needed   int     `json:"needed"`
	Match    float64 `json:"match"`
	Mismatch bool    `json:"mismatch"`
}

// Feed adds strokes and returns the current state.
func (m *Monitor) Feed(strokes []Stroke) State {
	m.mu.Lock()
	var fire func(float64)
	var score float64
	for _, s := range strokes {
		if !s.valid() {
			continue
		}
		if m.profile == nil {
			m.enroll = append(m.enroll, s)
			if p, ok := Enroll(m.enroll); ok {
				m.profile, m.enroll, m.last = &p, nil, 100
			}
			continue
		}
		m.window = append(m.window, s)
		if len(m.window) >= WindowN {
			m.last = Match(*m.profile, m.window)
			wd, wf := split(m.window)
			m.lastWin, m.haveWin = [2]dist{summarise(wd), summarise(wf)}, true
			m.window = m.window[:0]
			if m.last < 35 {
				m.low++
				if m.low == 2 {
					fire, score = m.OnMismatch, m.last
				}
			} else {
				m.low = 0
			}
		}
	}
	st := m.stateLocked()
	m.mu.Unlock()
	if fire != nil {
		fire(score)
	}
	return st
}

func (m *Monitor) stateLocked() State {
	if m.profile == nil {
		d, _ := split(m.enroll)
		return State{Progress: len(d), Needed: EnrollN, Match: 0}
	}
	return State{Enrolled: true, Progress: EnrollN, Needed: EnrollN, Match: math.Round(m.last), Mismatch: m.low >= 2}
}

// State returns the current state.
func (m *Monitor) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked()
}

// Reset forgets the profile.
func (m *Monitor) Reset() {
	m.mu.Lock()
	m.enroll, m.profile, m.window, m.low, m.last, m.haveWin = nil, nil, nil, 0, 0, false
	m.mu.Unlock()
}

// Saved returns the enrolled profile, or nil.
func (m *Monitor) Saved() *Profile {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profile == nil {
		return nil
	}
	p := *m.profile
	return &p
}

// Restore loads a saved profile.
func (m *Monitor) Restore(p *Profile) {
	if p == nil || p.N < EnrollN || p.Dwell.Std <= 0 || p.Flight.Std <= 0 {
		return
	}
	m.mu.Lock()
	c := *p
	m.profile, m.enroll, m.window, m.low, m.last = &c, nil, nil, 0, 100
	m.mu.Unlock()
}

// Drift compares the most recent window with the enrolled rhythm: how much
// slower the holds and gaps are (0.10 = 10% slower) and how much more
// variable. ok is false until a profile and one full window exist.
func (m *Monitor) Drift() (hold, gap, spread float64, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profile == nil || !m.haveWin || m.profile.Dwell.Mean <= 0 || m.profile.Flight.Mean <= 0 {
		return 0, 0, 0, false
	}
	hold = m.lastWin[0].Mean/m.profile.Dwell.Mean - 1
	gap = m.lastWin[1].Mean/m.profile.Flight.Mean - 1
	spread = (m.lastWin[0].Std/math.Max(m.profile.Dwell.Std, 1) + m.lastWin[1].Std/math.Max(m.profile.Flight.Std, 1)) / 2
	return hold, gap, spread, true
}
