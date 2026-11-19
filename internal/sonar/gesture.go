package sonar

import "time"

// GestureDetector recognises deliberate waves through a link: two or three
// short bursts of movement with brief stillness between them, followed by
// stillness. Walking through a link is one long burst and is ignored.
type GestureDetector struct {
	MaxBurst time.Duration // a burst longer than this is not a wave
	MinGap   time.Duration // stillness needed between waves
	MaxGap   time.Duration // ...and the most allowed; also how long to wait after the last wave
	st       map[string]*gestureState
}

type gestureState struct {
	moving bool
	start  time.Time
	ends   []time.Time // end of each qualifying burst
	begins []time.Time
}

// NewGestureDetector returns a detector with defaults sized for the engine's
// 1.6-second analysis window, which stretches every burst.
func NewGestureDetector() *GestureDetector {
	return &GestureDetector{MaxBurst: 3200 * time.Millisecond, MinGap: 400 * time.Millisecond, MaxGap: 3 * time.Second, st: map[string]*gestureState{}}
}

// Feed takes one reading of a link. It returns "double-wave" or "triple-wave"
// once a gesture has finished, otherwise "".
func (g *GestureDetector) Feed(node string, moving bool, now time.Time) string {
	s := g.st[node]
	if s == nil {
		s = &gestureState{}
		g.st[node] = s
	}
	switch {
	case moving && !s.moving: // a burst begins
		if n := len(s.ends); n > 0 {
			if gap := now.Sub(s.ends[n-1]); gap < g.MinGap || gap > g.MaxGap {
				s.ends, s.begins = nil, nil
			}
		}
		s.moving, s.start = true, now
	case !moving && s.moving: // a burst ends
		s.moving = false
		if now.Sub(s.start) <= g.MaxBurst {
			s.begins, s.ends = append(s.begins, s.start), append(s.ends, now)
		} else {
			s.ends, s.begins = nil, nil // that was someone walking
		}
	case moving && now.Sub(s.start) > g.MaxBurst:
		s.ends, s.begins = nil, nil
	case !moving && len(s.ends) > 0 && now.Sub(s.ends[len(s.ends)-1]) > g.MaxGap:
		n := len(s.ends)
		s.ends, s.begins = nil, nil
		if n == 2 {
			return "double-wave"
		}
		if n == 3 {
			return "triple-wave"
		}
	}
	return ""
}
