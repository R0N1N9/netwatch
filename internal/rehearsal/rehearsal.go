// Package rehearsal runs incident drills: a scripted, clearly labelled
// synthetic incident is played into the console and the operator's responses
// are timed and scored. Nothing in a rehearsal touches a real host.
package rehearsal

import (
	"fmt"
	"time"
)

// Label prefixes every alert a rehearsal injects.
const Label = "[REHEARSAL] "

// Step is one beat of a scenario: an alert to inject and, optionally, the
// response it calls for.
type Step struct {
	After    time.Duration `json:"-"`
	Severity string        `json:"severity"`
	Kind     string        `json:"kind"`
	Message  string        `json:"message"`
	Expect   string        `json:"expect"` // "" | investigate | reach | quarantine | note
	Hint     string        `json:"hint"`
	Within   time.Duration `json:"-"` // full marks if answered this fast
}

// Scenario is a scripted incident.
type Scenario struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Brief     string   `json:"brief"`
	Target    string   `json:"target"`    // intruder | gateway
	Forbidden []string `json:"forbidden"` // responses that make things worse
	Steps     []Step   `json:"steps"`
}

// Scenarios lists the built-in drills.
func Scenarios() []Scenario {
	s := time.Second
	return []Scenario{
		{ID: "ghost", Name: "GHOST IN THE SEGMENT", Target: "intruder",
			Brief: "An unknown device joins, starts calling out, then probes your decoys. Work out what it is and contain it.",
			Steps: []Step{
				{After: 2 * s, Severity: "WARN", Kind: "new-device", Message: "new host joined the segment · randomized MAC (privacy address or evasion)", Expect: "investigate", Hint: "Open the alert in CORTEX", Within: 30 * s},
				{After: 14 * s, Severity: "WARN", Kind: "beacon", Message: "periodic callback to 203.0.113.44:443 every ~60 s", Expect: "reach", Hint: "Look at what it talks to in ORACLE", Within: 40 * s},
				{After: 28 * s, Severity: "CRITICAL", Kind: "decoy", Message: "DECOY tripped: connected to the decoy port tcp/2323 — first bytes: root", Expect: "quarantine", Hint: "Quarantine it (BLACKWALL, or approve the CORTEX proposal)", Within: 30 * s},
				{After: 36 * s, Severity: "INFO", Kind: "anomaly", Message: "the host has gone quiet", Expect: "note", Hint: "Write down what you did in the LEDGER", Within: 60 * s},
			}},
		{ID: "mirror", Name: "MIRROR AT THE GATE", Target: "gateway", Forbidden: []string{"quarantine"},
			Brief: "Your gateway's hardware address changes. That is either an interception or a router reboot. Find out which without cutting everyone off.",
			Steps: []Step{
				{After: 2 * s, Severity: "CRITICAL", Kind: "arp-spoof", Message: "gateway MAC changed (possible MITM)", Expect: "investigate", Hint: "Open the alert in CORTEX and read its proposal", Within: 30 * s},
				{After: 12 * s, Severity: "WARN", Kind: "dns", Message: "the resolver is suddenly answering three names with new addresses", Expect: "reach", Hint: "Check what the gateway is talking to in ORACLE", Within: 40 * s},
				{After: 20 * s, Severity: "INFO", Kind: "anomaly", Message: "nothing further — the change has held for 20 seconds", Expect: "note", Hint: "Verify the router yourself, then record what you found in the LEDGER", Within: 90 * s},
			}},
	}
}

// Find returns the scenario with that id.
func Find(id string) (Scenario, bool) {
	for _, s := range Scenarios() {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

// StepState is a step with what has happened to it.
type StepState struct {
	Step
	Injected int64   `json:"injected"` // unix ms, 0 = not yet
	Done     int64   `json:"done"`     // unix ms the expected response arrived
	Seconds  float64 `json:"seconds"`  // how long the response took
	Points   int     `json:"points"`
	Max      int     `json:"max"`
}

// State is what the console shows.
type State struct {
	Active    bool        `json:"active"`
	Scenario  Scenario    `json:"scenario"`
	Host      string      `json:"host"`
	Started   int64       `json:"started"`
	Steps     []StepState `json:"steps"`
	Penalties []string    `json:"penalties"`
	Score     int         `json:"score"`
	Finished  bool        `json:"finished"`
	Verdict   string      `json:"verdict"`
}

// Session is one run of a scenario.
type Session struct {
	sc        Scenario
	host      string
	start     time.Time
	steps     []StepState
	penalties []string
	lost      int
	finished  bool
}

// Start begins a scenario against host (the synthetic intruder or the gateway).
func Start(sc Scenario, host string, now time.Time) *Session {
	s := &Session{sc: sc, host: host, start: now}
	expected := 0
	for _, st := range sc.Steps {
		if st.Expect != "" {
			expected++
		}
	}
	for _, st := range sc.Steps {
		ss := StepState{Step: st}
		if st.Expect != "" {
			ss.Max = 100 / expected
		}
		s.steps = append(s.steps, ss)
	}
	return s
}

// Host is the address the drill is about.
func (s *Session) Host() string { return s.host }

// Due returns the steps whose moment has come and marks them injected.
func (s *Session) Due(now time.Time) []Step {
	var out []Step
	for i := range s.steps {
		if s.steps[i].Injected == 0 && now.Sub(s.start) >= s.steps[i].After {
			s.steps[i].Injected = now.UnixMilli()
			out = append(out, s.steps[i].Step)
		}
	}
	return out
}

// Act records something the operator did to the drill's host. It reports
// whether the action was one the drill was waiting for, and any penalty text.
func (s *Session) Act(action string, now time.Time) (matched bool, penalty string) {
	if s.finished {
		return false, ""
	}
	for _, f := range s.sc.Forbidden {
		if f == action {
			s.lost += 15
			penalty = fmt.Sprintf("%s was the wrong call here (−15)", action)
			s.penalties = append(s.penalties, penalty)
			return false, penalty
		}
	}
	for i := range s.steps {
		st := &s.steps[i]
		if st.Expect != action || st.Injected == 0 || st.Done != 0 {
			continue
		}
		st.Done = now.UnixMilli()
		took := now.Sub(time.UnixMilli(st.Injected))
		st.Seconds = took.Seconds()
		// Full marks inside the window, sliding to 40% at three times the window.
		frac := 1.0
		if took > st.Within {
			frac = 1 - 0.6*float64(took-st.Within)/float64(2*st.Within)
			if frac < 0.4 {
				frac = 0.4
			}
		}
		st.Points = int(float64(st.Max)*frac + 0.5)
		return true, ""
	}
	return false, ""
}

// Tick ends the session once everything expected is done, or 90 seconds after
// the last step if the operator never finished. It reports whether it just ended.
func (s *Session) Tick(now time.Time) bool {
	if s.finished {
		return false
	}
	all, last := true, time.Duration(0)
	for _, st := range s.steps {
		if st.Expect != "" && st.Done == 0 {
			all = false
		}
		if st.After > last {
			last = st.After
		}
	}
	if all || now.Sub(s.start) > last+90*time.Second {
		s.finished = true
		return true
	}
	return false
}

// Stop ends the session now.
func (s *Session) Stop() { s.finished = true }

// Score is the running total, 0 to 100.
func (s *Session) Score() int {
	total := 0
	for _, st := range s.steps {
		total += st.Points
	}
	if total -= s.lost; total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}
	return total
}

// State returns a copy for the console.
func (s *Session) State() State {
	st := State{Active: !s.finished, Scenario: s.sc, Host: s.host, Started: s.start.UnixMilli(), Steps: append([]StepState(nil), s.steps...),
		Penalties: append([]string{}, s.penalties...), Score: s.Score(), Finished: s.finished}
	if s.finished {
		switch sc := st.Score; {
		case sc >= 85:
			st.Verdict = "clean run"
		case sc >= 60:
			st.Verdict = "contained, but slow"
		case sc >= 30:
			st.Verdict = "it got further than it should have"
		default:
			st.Verdict = "the incident ran its course"
		}
	}
	return st
}
