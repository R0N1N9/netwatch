// Package recap notices when nobody has been around for a while and, when
// someone returns, summarises what happened in between.
package recap

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Span is a stretch of absence.
type Span struct{ From, To time.Time }

// Tracker turns a stream of "someone is here" readings into absences.
type Tracker struct {
	AwayAfter time.Duration // no presence for this long counts as away
	last      time.Time
	away      bool
}

// Feed takes one reading. It returns a span when someone comes back after
// being away.
func (t *Tracker) Feed(present bool, now time.Time) *Span {
	if t.last.IsZero() {
		t.last = now
	}
	if !present {
		if !t.away && now.Sub(t.last) >= t.AwayAfter {
			t.away = true
		}
		return nil
	}
	var out *Span
	if t.away {
		out = &Span{From: t.last, To: now}
		t.away = false
	}
	t.last = now
	return out
}

// Away reports whether the tracker currently considers the place empty.
func (t *Tracker) Away() bool { return t.away }

// Event is one thing that happened.
type Event struct {
	T        int64  `json:"t"`
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Text     string `json:"text"`
}

// Recap is the summary shown on return.
type Recap struct {
	From     int64    `json:"from"`
	To       int64    `json:"to"`
	Minutes  float64  `json:"minutes"`
	Critical int      `json:"critical"`
	Warnings int      `json:"warnings"`
	Joined   []string `json:"joined"`
	Left     []string `json:"left"`
	Moves    int      `json:"moves"` // movement onsets sensed in the room
	Events   []Event  `json:"events"`
	Headline string   `json:"headline"`
}

// Build summarises a span. events may cover more than the span; before and
// after are the hosts present at each end.
func Build(span Span, events []Event, before, after []string, moves int) Recap {
	r := Recap{From: span.From.UnixMilli(), To: span.To.UnixMilli(), Minutes: span.To.Sub(span.From).Minutes(), Moves: moves,
		Joined: diff(after, before), Left: diff(before, after), Events: []Event{}}
	for _, e := range events {
		if e.T < r.From || e.T > r.To {
			continue
		}
		r.Events = append(r.Events, e)
		switch e.Severity {
		case "CRITICAL":
			r.Critical++
		case "WARN":
			r.Warnings++
		}
	}
	sort.Slice(r.Events, func(i, j int) bool { return r.Events[i].T < r.Events[j].T })

	var parts []string
	if r.Critical > 0 {
		parts = append(parts, plural(r.Critical, "critical alert"))
	}
	if r.Warnings > 0 {
		parts = append(parts, plural(r.Warnings, "warning"))
	}
	if len(r.Joined) > 0 {
		parts = append(parts, plural(len(r.Joined), "new device"))
	}
	if len(r.Left) > 0 {
		parts = append(parts, plural(len(r.Left), "device")+" gone")
	}
	if moves > 0 {
		parts = append(parts, "movement sensed "+plural(moves, "time"))
	}
	away := fmt.Sprintf("%.0f minutes", r.Minutes)
	if r.Minutes >= 90 {
		away = fmt.Sprintf("%.1f hours", r.Minutes/60)
	}
	if len(parts) == 0 {
		r.Headline = "Quiet. Nothing happened in the " + away + " you were away."
	} else {
		r.Headline = "While you were out (" + away + "): " + strings.Join(parts, ", ") + "."
	}
	return r
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// diff returns the members of a that are not in b.
func diff(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	out := []string{}
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
