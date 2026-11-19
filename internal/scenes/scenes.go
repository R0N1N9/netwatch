// Package scenes holds "when this happens, do that" rules for the room and
// the network: a trigger, an optional match, and a list of actions. It only
// decides which scenes fire; carrying out the actions is the server's job.
package scenes

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Triggers and actions a scene may use.
var (
	Triggers = []string{"tripwire", "presence", "alert", "away", "return", "gesture"}
	Actions  = []string{"clip", "speak", "alert", "note", "light", "ack", "arm", "disarm", "next-deck", "lock"}
)

// Scene is one rule.
type Scene struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Trigger   string   `json:"trigger"`
	Match     string   `json:"match"`     // tripwire name, node, alert kind or severity, gesture; "" matches any
	ArmedOnly bool     `json:"armedOnly"` // only while SONAR is armed
	Actions   []string `json:"actions"`
	Say       string   `json:"say"`      // what "speak" says
	Cooldown  int      `json:"cooldown"` // seconds before it may fire again
	Off       bool     `json:"off"`
}

// Event is something that happened.
type Event struct {
	Trigger string
	Subject string // what it happened to: the wire, node, alert kind, gesture
	Extra   string // a second thing to match on (an alert's severity)
	Armed   bool
}

func has(list []string, x string) bool {
	for _, y := range list {
		if x == y {
			return true
		}
	}
	return false
}

// Validate checks a scene and fills in defaults.
func Validate(sc *Scene) error {
	sc.Name, sc.Match, sc.Say = strings.TrimSpace(sc.Name), strings.TrimSpace(sc.Match), strings.TrimSpace(sc.Say)
	if sc.Name == "" || len(sc.Name) > 60 {
		return fmt.Errorf("a scene needs a name of 1 to 60 characters")
	}
	if !has(Triggers, sc.Trigger) {
		return fmt.Errorf("unknown trigger %q", sc.Trigger)
	}
	if len(sc.Actions) == 0 || len(sc.Actions) > 6 {
		return fmt.Errorf("a scene needs 1 to 6 actions")
	}
	for _, a := range sc.Actions {
		if !has(Actions, a) {
			return fmt.Errorf("unknown action %q", a)
		}
	}
	if len(sc.Match) > 60 || len(sc.Say) > 200 {
		return fmt.Errorf("match or speech text is too long")
	}
	if has(sc.Actions, "speak") && sc.Say == "" {
		sc.Say = sc.Name
	}
	if sc.Cooldown < 5 {
		sc.Cooldown = 5 // a scene can never fire in a tight loop
	}
	if sc.Cooldown > 86400 {
		sc.Cooldown = 86400
	}
	return nil
}

// Engine holds the scenes and their cooldowns.
type Engine struct {
	mu     sync.Mutex
	scenes []Scene
	last   map[string]time.Time
	seq    int
}

// New returns an empty engine.
func New() *Engine { return &Engine{last: map[string]time.Time{}} }

// List returns a copy of the scenes.
func (e *Engine) List() []Scene {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Scene{}, e.scenes...)
}

// Put adds a scene, or replaces the one with the same id.
func (e *Engine) Put(sc Scene) (Scene, error) {
	if err := Validate(&sc); err != nil {
		return sc, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.scenes {
		if e.scenes[i].ID == sc.ID && sc.ID != "" {
			e.scenes[i] = sc
			return sc, nil
		}
	}
	if len(e.scenes) >= 40 {
		return sc, fmt.Errorf("that is enough scenes (40)")
	}
	if sc.ID == "" {
		e.seq++
		sc.ID = fmt.Sprintf("scene-%d-%d", time.Now().Unix()%100000, e.seq)
	}
	e.scenes = append(e.scenes, sc)
	return sc, nil
}

// Remove deletes a scene.
func (e *Engine) Remove(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	keep := e.scenes[:0]
	for _, sc := range e.scenes {
		if sc.ID != id {
			keep = append(keep, sc)
		}
	}
	e.scenes = keep
}

// Get returns one scene.
func (e *Engine) Get(id string) (Scene, bool) {
	for _, sc := range e.List() {
		if sc.ID == id {
			return sc, true
		}
	}
	return Scene{}, false
}

// Uses reports whether any enabled scene has that trigger.
func (e *Engine) Uses(trigger string) bool {
	for _, sc := range e.List() {
		if sc.Trigger == trigger && !sc.Off {
			return true
		}
	}
	return false
}

// Fire returns the scenes an event sets off, and starts their cooldowns.
func (e *Engine) Fire(ev Event, now time.Time) []Scene {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Scene
	for _, sc := range e.scenes {
		if sc.Off || sc.Trigger != ev.Trigger || (sc.ArmedOnly && !ev.Armed) {
			continue
		}
		if sc.Match != "" && !strings.EqualFold(sc.Match, ev.Subject) && !strings.EqualFold(sc.Match, ev.Extra) {
			continue
		}
		if last, ok := e.last[sc.ID]; ok && now.Sub(last) < time.Duration(sc.Cooldown)*time.Second {
			continue
		}
		e.last[sc.ID] = now
		out = append(out, sc)
	}
	return out
}
