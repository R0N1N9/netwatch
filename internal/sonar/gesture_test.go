package sonar

import (
	"testing"
	"time"
)

// play feeds a script of (moving, seconds) segments at 5 Hz and returns every gesture reported.
func play(g *GestureDetector, script ...float64) []string {
	var out []string
	now := time.Unix(1_700_000_000, 0)
	moving := false
	for _, secs := range script {
		for t := 0.0; t < secs; t += 0.2 {
			if r := g.Feed("node-A", moving, now); r != "" {
				out = append(out, r)
			}
			now = now.Add(200 * time.Millisecond)
		}
		moving = !moving
	}
	return out
}

func TestGestures(t *testing.T) {
	// still, wave, pause, wave, still
	if got := play(NewGestureDetector(), 2, 1.6, 1, 1.8, 5); len(got) != 1 || got[0] != "double-wave" {
		t.Errorf("two waves read as %v", got)
	}
	if got := play(NewGestureDetector(), 2, 1.6, 1, 1.6, 1.2, 1.6, 5); len(got) != 1 || got[0] != "triple-wave" {
		t.Errorf("three waves read as %v", got)
	}
	for name, script := range map[string][]float64{
		"one wave":                {2, 1.6, 8},
		"walking through":         {2, 6, 8},
		"walk, then a wave":       {2, 6, 1, 1.6, 8},
		"two waves far apart":     {2, 1.6, 6, 1.6, 8},
		"four bursts (fidgeting)": {2, 1.2, 1, 1.2, 1, 1.2, 1, 1.2, 8},
	} {
		if got := play(NewGestureDetector(), script...); len(got) != 0 {
			t.Errorf("%s read as %v", name, got)
		}
	}
}
