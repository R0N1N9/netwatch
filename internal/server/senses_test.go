package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/r0n1n9/netwatch/internal/recap"
	"github.com/r0n1n9/netwatch/internal/rehearsal"
)

func TestRehearsalTouchesNothingReal(t *testing.T) {
	s := demoServer(t)
	s.demo = false // prove the drill itself keeps away from the firewall, not just demo mode
	hosts, ledger := len(s.devices), s.mod.led.Len()
	if r := call(t, s.handleRehearsal, `{"start":"ghost"}`); r["ok"] != true {
		t.Fatalf("start: %v", r)
	}
	if r := call(t, s.handleRehearsal, `{"start":"mirror"}`); r["ok"] != false {
		t.Error("two rehearsals at once")
	}
	ghost := s.rehearsalIntruder()
	if len(s.devices) != hosts+1 || s.devices[hosts].Hostname != "REHEARSAL-GHOST" || ghost != "192.168.1.250" {
		t.Fatalf("synthetic host missing: %v", s.devices[len(s.devices)-1])
	}
	// play every step at once, then answer them
	s.rehearsalTick(time.Now().Add(40 * time.Second))
	var first int64
	n := 0
	for _, a := range s.alerts {
		if a.Source == ghost {
			n++
			first = a.Time.UnixMilli()
			if !strings.HasPrefix(a.Message, rehearsal.Label) {
				t.Errorf("unlabelled rehearsal alert: %q", a.Message)
			}
		}
	}
	if n != 4 {
		t.Fatalf("%d rehearsal alerts, want 4", n)
	}
	b, _ := json.Marshal(map[string]any{"ts": first, "source": ghost, "kind": s.alerts[0].Kind})
	if r := call(t, s.handleCortex, string(b)); r["ok"] != true {
		t.Fatalf("investigate: %v", r)
	}
	getJSON(t, s.handleOracle, "/api/oracle?by=operator&host="+ghost)
	if r := call(t, s.handleBlackwall, `{"ip":"`+ghost+`","action":"quarantine"}`); r["ok"] != true || !strings.Contains(r["note"].(string), "simulated") {
		t.Fatalf("quarantine: %v", r)
	}
	if len(s.wall.List()) != 0 {
		t.Fatalf("the rehearsal reached the firewall: %v", s.wall.List())
	}
	call(t, s.handleLedgerNote, `{"text":"contained the ghost"}`)
	s.rehearsalTick(time.Now())
	st := getJSON(t, s.handleRehearsal, "/api/rehearsal")
	state := st["state"].(map[string]any)
	if state["finished"] != true || state["score"].(float64) < 85 || len(st["scores"].([]any)) != 1 {
		t.Errorf("after answering everything: finished=%v score=%v", state["finished"], state["score"])
	}
	if len(s.devices) != hosts || s.quarantined[ghost] {
		t.Error("the synthetic host or its quarantine outlived the rehearsal")
	}
	if s.mod.led.Len() <= ledger+3 || s.mod.led.Verify() != 0 {
		t.Errorf("ledger went from %d to %d entries", ledger, s.mod.led.Len())
	}
}

func TestRehearsalRefusesToCutOffTheGateway(t *testing.T) {
	s := demoServer(t)
	s.demo = false
	if r := call(t, s.handleRehearsal, `{"start":"mirror"}`); r["ok"] != true {
		t.Fatalf("start: %v", r)
	}
	s.rehearsalTick(time.Now().Add(30 * time.Second))
	r := call(t, s.handleBlackwall, `{"ip":"192.168.1.1","action":"quarantine"}`)
	if r["ok"] != false || !strings.Contains(r["error"].(string), "wrong call") || len(s.wall.List()) != 0 || s.quarantined["192.168.1.1"] {
		t.Errorf("gateway quarantine during the drill: %v", r)
	}
	call(t, s.handleRehearsal, `{"stop":true}`)
	if st := getJSON(t, s.handleRehearsal, "/api/rehearsal")["state"].(map[string]any); st["finished"] != true || len(st["penalties"].([]any)) != 1 {
		t.Errorf("state after stop = %v", st)
	}
}

func TestAckRecapAndOperator(t *testing.T) {
	s := demoServer(t)
	// operator watch is off until asked for
	if o := getJSON(t, s.handleOperator, "/api/operator"); o["on"] != false || o["reading"] != nil {
		t.Errorf("operator watch default = %v", o)
	}
	if r := call(t, s.handleAck, `{"newest":true}`); r["acknowledged"].(float64) != 1 {
		t.Errorf("ack newest: %v", r)
	}
	var snap struct {
		Alerts []wireAlert `json:"alerts"`
	}
	json.Unmarshal(s.snapshotJSON(), &snap)
	if !snap.Alerts[0].Acked || snap.Alerts[1].Acked {
		t.Errorf("acked flags = %v, %v", snap.Alerts[0].Acked, snap.Alerts[1].Acked)
	}

	// the demo has 30 minutes of history: two hosts joined and five alerts fired in it
	r := call(t, s.handleRecap, `{"mins":31}`)["recap"].(map[string]any)
	if len(r["joined"].([]any)) != 2 || r["critical"].(float64) != 2 || !strings.Contains(r["headline"].(string), "2 new devices") {
		t.Errorf("recap = %v", r["headline"])
	}
	if seq := getJSON(t, s.handleRecap, "/api/recap")["seq"].(float64); seq != 1 {
		t.Errorf("recap seq = %v", seq)
	}
	// an absence noticed through operator activity, with no sensor nodes
	s.sen.opOn, s.sen.opLast = true, time.Now().Add(-20*time.Minute)
	now := time.Now()
	s.sen.away = recap.Tracker{AwayAfter: 10 * time.Minute}
	s.sen.away.Feed(true, now.Add(-20*time.Minute))
	s.presenceTick(now.Add(-time.Minute)) // still nobody
	s.sen.opLast = now
	s.presenceTick(now) // back
	if seq := getJSON(t, s.handleRecap, "/api/recap")["seq"].(float64); seq != 2 {
		t.Errorf("returning after 20 minutes did not produce a recap (seq %v)", seq)
	}

	s.sen.opOn = false
	o := call(t, s.handleOperator, `{"on":true}`)
	if o["on"] != true || o["reading"].(map[string]any)["sharpness"].(float64) > 100 {
		t.Errorf("operator on = %v", o)
	}
	s.sen.opStart = time.Now().Add(-5 * time.Hour) // five hours in, with critical alerts unopened
	s.sen.opLast = time.Now()
	before := len(s.alerts)
	s.operatorTick(time.Date(2026, 1, 1, 3, 0, 0, 0, time.Local))
	if s.sen.opReading.Level != "spent" || len(s.alerts) != before+1 || s.alerts[0].Kind != "operator" {
		t.Errorf("reading = %+v", s.sen.opReading)
	}

	sc := getJSON(t, s.handleScanner, "/api/scanner")["hosts"].([]any)
	for _, h := range sc {
		if m := h.(map[string]any); m["ip"] == "192.168.1.99" && (m["verdict"] != "likely threat" || m["trips"].(float64) != 1 || m["outside"].(float64) != 2) {
			t.Errorf("scanner row for the unverified host = %v", m)
		}
	}
	if len(sc) != 5 {
		t.Errorf("%d scanner rows", len(sc))
	}
}

func TestConsoleServesItsIcon(t *testing.T) {
	s := demoServer(t)
	for path, ctype := range map[string]string{"/favicon.svg": "image/svg+xml", "/favicon.png": "image/png", "/apple-touch-icon.png": "image/png"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), ctype) || w.Body.Len() < 500 {
			t.Errorf("%s: %d %q %d bytes", path, w.Code, w.Header().Get("Content-Type"), w.Body.Len())
		}
	}
}
