package scenes

import (
	"testing"
	"time"
)

func TestFireMatchCooldownAndArmed(t *testing.T) {
	e := New()
	hall, err := e.Put(Scene{Name: "Hallway", Trigger: "tripwire", Match: "hallway", ArmedOnly: true, Actions: []string{"clip", "speak"}, Cooldown: 30})
	if err != nil || hall.Say != "Hallway" || hall.ID == "" {
		t.Fatalf("put: %+v %v", hall, err)
	}
	e.Put(Scene{Name: "Any critical", Trigger: "alert", Match: "critical", Actions: []string{"light"}})
	e.Put(Scene{Name: "Off", Trigger: "tripwire", Actions: []string{"note"}, Off: true})
	for _, bad := range []Scene{{Name: "x", Trigger: "weather", Actions: []string{"clip"}}, {Name: "x", Trigger: "alert", Actions: []string{"format-disk"}}, {Name: "x", Trigger: "alert"}, {Trigger: "alert", Actions: []string{"clip"}}} {
		if _, err := e.Put(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	now := time.Unix(1_700_000_000, 0)
	if got := e.Fire(Event{Trigger: "tripwire", Subject: "hallway"}, now); len(got) != 0 {
		t.Error("an armed-only scene fired while disarmed")
	}
	if got := e.Fire(Event{Trigger: "tripwire", Subject: "Hallway", Armed: true}, now); len(got) != 1 || got[0].Name != "Hallway" {
		t.Errorf("hallway crossing fired %v", got)
	}
	if got := e.Fire(Event{Trigger: "tripwire", Subject: "hallway", Armed: true}, now.Add(10*time.Second)); len(got) != 0 {
		t.Error("fired again inside its cooldown")
	}
	if got := e.Fire(Event{Trigger: "tripwire", Subject: "garden", Armed: true}, now.Add(time.Minute)); len(got) != 0 {
		t.Error("fired for a different wire")
	}
	if got := e.Fire(Event{Trigger: "alert", Subject: "decoy", Extra: "CRITICAL"}, now); len(got) != 1 || got[0].Cooldown != 5 {
		t.Errorf("critical alert fired %v", got)
	}
	if !e.Uses("alert") || e.Uses("gesture") {
		t.Error("Uses is wrong")
	}
	e.Remove(hall.ID)
	if len(e.List()) != 2 {
		t.Errorf("%d scenes after removing one of three", len(e.List()))
	}
}
