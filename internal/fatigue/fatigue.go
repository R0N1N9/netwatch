// Package fatigue estimates how sharp the operator is from things the
// console already knows: how long they have been at it, the hour, drift in
// their typing rhythm and alerts left unopened. It is a nudge, not a diagnosis.
package fatigue

import "fmt"

// Input is what is known about the operator right now.
type Input struct {
	SessionMin  float64 // minutes of continuous activity without a 15-minute break
	Hour        int     // local hour, 0-23
	HaveRhythm  bool    // a typing profile exists and a recent window was measured
	HoldDrift   float64 // recent key hold time relative to the enrolled one, minus 1 (0.10 = 10% slower)
	GapDrift    float64 // the same for the gap between keys
	SpreadRatio float64 // recent variability over enrolled variability
	Unopened    int     // critical alerts nobody has opened
	OldestMin   float64 // age of the oldest of those, in minutes
}

// Factor is one thing costing sharpness.
type Factor struct {
	Name   string `json:"name"`
	Cost   int    `json:"cost"`
	Detail string `json:"detail"`
}

// Reading is the estimate.
type Reading struct {
	Sharpness int      `json:"sharpness"` // 100 = fresh
	Level     string   `json:"level"`     // sharp | tiring | spent
	Factors   []Factor `json:"factors"`
	Advice    string   `json:"advice"`
}

func clamp(v, lo, hi float64) int {
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return int(v + 0.5)
}

// Assess turns the inputs into a reading.
func Assess(in Input) Reading {
	r := Reading{Factors: []Factor{}}
	add := func(name string, cost int, detail string) {
		if cost > 0 {
			r.Factors = append(r.Factors, Factor{name, cost, detail})
		}
	}
	if in.SessionMin > 90 {
		add("Time at the console", clamp((in.SessionMin-90)/5, 0, 35), fmt.Sprintf("%.1f hours without a real break", in.SessionMin/60))
	}
	switch {
	case in.Hour < 6:
		add("Hour", 25, fmt.Sprintf("it is %02d:00 — the worst hours for judgement", in.Hour))
	case in.Hour >= 22:
		add("Hour", 8, "it is late")
	}
	if in.HaveRhythm {
		slow := (in.HoldDrift + in.GapDrift) / 2
		if slow > 0.08 {
			add("Typing has slowed", clamp((slow-0.08)*150, 0, 20), fmt.Sprintf("keys are %.0f%% slower than your enrolled rhythm", slow*100))
		}
		if in.SpreadRatio > 1.25 {
			add("Typing is uneven", clamp((in.SpreadRatio-1.25)*30, 0, 15), fmt.Sprintf("%.1f× your usual variability", in.SpreadRatio))
		}
	}
	if in.Unopened > 0 {
		cost := clamp(float64(in.Unopened)*5, 0, 15)
		if in.OldestMin > 10 {
			cost += 10
		}
		add("Alerts left unopened", cost, fmt.Sprintf("%d critical, the oldest %.0f minutes old", in.Unopened, in.OldestMin))
	}
	total := 0
	for _, f := range r.Factors {
		total += f.Cost
	}
	if r.Sharpness = 100 - total; r.Sharpness < 0 {
		r.Sharpness = 0
	}
	switch {
	case r.Sharpness >= 70:
		r.Level, r.Advice = "sharp", "You are fine to make calls."
	case r.Sharpness >= 40:
		r.Level, r.Advice = "tiring", "Take ten minutes away before anything you cannot undo."
	default:
		r.Level, r.Advice = "spent", "Do not quarantine or trust anything now unless it cannot wait. Write a note in the LEDGER and come back rested."
	}
	return r
}
