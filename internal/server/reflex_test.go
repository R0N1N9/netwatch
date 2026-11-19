package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/r0n1n9/netwatch/internal/braindance"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/sonar"
)

func TestScenesFireFromTriggersAndGestures(t *testing.T) {
	s := demoServer(t)
	s.demoReflex()
	v := getJSON(t, s.handleReflex, "/api/reflex")
	if len(v["scenes"].([]any)) != 3 || v["listening"] != true {
		t.Fatalf("demo scenes = %v listening = %v", len(v["scenes"].([]any)), v["listening"])
	}
	if r := call(t, s.handleReflex, `{"put":{"name":"bad","trigger":"alert","actions":["format-disk"]}}`); r["ok"] != false {
		t.Error("accepted an unknown action")
	}
	if r := call(t, s.handleReflex, `{"put":{"name":"Decoy: lock and note","trigger":"alert","match":"decoy","actions":["lock","note","speak"],"say":"Decoy touched."}}`); r["ok"] != true {
		t.Fatalf("put: %v", r)
	}
	// a critical decoy alert sets off two scenes: the new one and "Critical: flash the light"
	ledger := s.mod.led.Len()
	s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevCritical, Kind: "decoy", Source: "192.168.1.99", Message: "DECOY tripped"})
	fired := getJSON(t, s.handleReflex, "/api/reflex")["fired"].([]any)
	if len(fired) != 2 {
		t.Fatalf("%d scenes fired, want 2: %v", len(fired), fired)
	}
	did := fmt.Sprint(fired)
	if !strings.Contains(did, "demo: not locked") || !strings.Contains(did, "flashed the light") {
		t.Errorf("fired = %v", did)
	}
	var snap struct {
		Meta struct {
			Cue struct {
				Seq int    `json:"seq"`
				Say string `json:"say"`
			} `json:"cue"`
		} `json:"meta"`
	}
	json.Unmarshal(s.snapshotJSON(), &snap)
	if snap.Meta.Cue.Seq != 1 || snap.Meta.Cue.Say != "Decoy touched." {
		t.Errorf("cue = %+v", snap.Meta.Cue)
	}
	if s.mod.led.Len() < ledger+4 {
		t.Errorf("ledger grew by %d (alert, note and two scene records expected)", s.mod.led.Len()-ledger)
	}
	// a wave acknowledges the newest alert
	unacked := func() int {
		n := 0
		for _, a := range s.alerts {
			if !s.sen.acked[alertKey(a)] {
				n++
			}
		}
		return n
	}
	before := unacked()
	if r := call(t, s.handleReflex, `{"gesture":"double-wave"}`); r["ok"] != true {
		t.Fatalf("gesture: %v", r)
	}
	if unacked() != before-1 {
		t.Errorf("a double wave acknowledged %d alerts, want 1", before-unacked())
	}
	// a tripwire only fires its scene while armed
	n := len(getJSON(t, s.handleReflex, "/api/reflex")["fired"].([]any))
	s.tripped(sonar.Wire{ID: "w1", Name: "hallway"}, time.Now())
	if got := len(getJSON(t, s.handleReflex, "/api/reflex")["fired"].([]any)); got != n {
		t.Error("the hallway scene fired while disarmed")
	}
	s.sonarArmed = true
	s.tripped(sonar.Wire{ID: "w2", Name: "hallway"}, time.Now())
	if got := len(getJSON(t, s.handleReflex, "/api/reflex")["fired"].([]any)); got <= n {
		t.Error("the hallway scene did not fire while armed")
	}
}

func TestThreatLightAddressAndDatagram(t *testing.T) {
	s := demoServer(t)
	s.demo = false
	if r := call(t, s.handleReflex, `{"lightAddr":"8.8.8.8:5577"}`); r["ok"] != false {
		t.Error("accepted a public address for the light")
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback UDP here")
	}
	defer pc.Close()
	if r := call(t, s.handleReflex, `{"lightAddr":"`+pc.LocalAddr().String()+`"}`); r["ok"] != true {
		t.Fatalf("set light: %v", r)
	}
	s.lightTick(time.Now())
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _, err := pc.ReadFrom(buf)
	var c struct {
		R     int    `json:"r"`
		Pulse string `json:"pulse"`
	}
	if err != nil || json.Unmarshal(buf[:n], &c) != nil || c.R != 255 || c.Pulse != "breathe" {
		t.Errorf("light got %q (%v) — the demo data has unopened critical alerts, so red and breathing was expected", buf[:n], err)
	}
}

func TestTalkingDecoyThroughTheConsole(t *testing.T) {
	s := testServer(t)
	s.demo = false
	t.Setenv("HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback networking here")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if r := call(t, s.handleDecoy, fmt.Sprintf(`{"action":"talk-open","port":%d}`, port)); r["ok"] != true {
		t.Fatalf("open: %v", r)
	}
	t.Cleanup(s.ref.per.CloseAll)
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	for _, line := range []string{"root", "S3cret-pass", "root", "S3cret-pass", "id", "exit"} {
		for { // wait for a prompt, then answer it
			b, err := r.ReadByte()
			if err != nil {
				t.Fatalf("connection ended early: %v", err)
			}
			if (b == ' ' && r.Buffered() == 0) || b == '#' {
				break
			}
		}
		fmt.Fprint(c, line+"\r\n")
	}
	c.Close()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		s.mu.Lock()
		n := len(s.alerts)
		s.mu.Unlock()
		if n > 0 {
			break
		}
	}
	v := getJSON(t, s.handleDecoy, "/api/decoy")
	body, _ := json.Marshal(v)
	if len(v["visits"].([]any)) != 1 || len(v["talking"].([]any)) != 1 {
		t.Fatalf("decoy view = %s", body)
	}
	if strings.Contains(string(body), "S3cret") {
		t.Error("the visitor's password is in the console's data")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.alerts) != 1 || s.alerts[0].Kind != "decoy" || !strings.Contains(s.alerts[0].Message, "2 login attempt(s) as root") {
		t.Errorf("alerts = %+v", s.alerts)
	}
}

func TestNewHostKeepsTheRoomAndHuntReads(t *testing.T) {
	s := demoServer(t)
	now := time.Now()
	for i := 20; i > 0; i-- { // ten seconds of room history
		s.bd.Add(braindance.Sample{T: now.Add(-time.Duration(i) * 500 * time.Millisecond).UnixMilli(), Nodes: []braindance.NodeSample{{Node: "node-A", State: "motion", Score: 80}}})
	}
	s.sen.motion = append(s.sen.motion, now.Add(-12*time.Second))
	a := model.Alert{Time: now, Severity: model.SevWarn, Kind: "new-device", Source: "192.168.1.77", Message: "new host joined the segment: 192.168.1.77"}
	s.emitAlert(a)
	var snap struct {
		Alerts []wireAlert `json:"alerts"`
	}
	json.Unmarshal(s.snapshotJSON(), &snap)
	if snap.Alerts[0].Clip == "" || len(s.bd.List()) != 1 {
		t.Fatalf("no clip was attached to the new-host alert: %+v", snap.Alerts[0])
	}
	if ev := s.evidence(a); !strings.Contains(ev.Room, "12 s before it joined") {
		t.Errorf("room evidence = %q", ev.Room)
	}
	s.emitAlert(model.Alert{Time: now, Severity: model.SevWarn, Kind: "new-device", Source: "192.168.1.250", Message: "[REHEARSAL] new host joined"})
	if len(s.bd.List()) != 1 {
		t.Error("a rehearsal alert saved a clip of the room")
	}

	frame := `{"node":"wand","seen":[{"mac":"de:ad:be:ef:00:01","rssi":-47}]}`
	s.handleSonarIngest(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(frame)))
	h := getJSON(t, s.handleSpectrum, "/api/spectrum?hunt=DE:AD:BE:EF:00:01")["hunt"].(map[string]any)
	if h["lost"] != false || h["readings"].([]any)[0].(map[string]any)["rssi"].(float64) != -47 {
		t.Errorf("hunt = %v", h)
	}
}

func TestCompanionNeedsThePairingCode(t *testing.T) {
	s := demoServer(t)
	s.comp = companion{on: true, code: "424242", port: "7443"}
	s.CompanionAddr = "192.168.1.50:7443"
	h := s.companionHandler()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if w := get("/"); w.Code != 200 || !strings.Contains(w.Body.String(), "NetWatch") {
		t.Errorf("companion page: %d", w.Code)
	}
	if w := get("/favicon.svg"); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "svg") {
		t.Errorf("companion favicon: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for _, p := range []string{"/api/blackwall", "/api/stream", "/app.js", "/index.html"} {
		if w := get(p); w.Code != 404 {
			t.Errorf("%s is reachable through the companion listener (%d)", p, w.Code)
		}
	}
	if w := get("/sonar"); w.Code != 403 {
		t.Errorf("/sonar without a code: %d", w.Code)
	}
	w := get("/sonar?code=424242")
	var v map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v["est"] == nil {
		t.Errorf("/sonar with the code: %d %s", w.Code, w.Body.String())
	}
	for i := 0; i < 12; i++ {
		get(fmt.Sprintf("/sonar?code=%06d", i))
	}
	if w := get("/sonar?code=424242"); w.Code != http.StatusTooManyRequests {
		t.Errorf("after a burst of wrong codes even the right one should wait: %d", w.Code)
	}
	if v := s.companionView(); v["url"] != "https://192.168.1.50:7443/?code=424242" {
		t.Errorf("companion url = %v", v["url"])
	}
}

func TestFinderPlacesHostsAndThePhoneItself(t *testing.T) {
	s := demoServer(t)
	s.demoRoom() // node positions
	now := time.Now()
	sims := []*sonar.Sim{sonar.NewSim("node-A", 1), sonar.NewSim("node-B", 2), sonar.NewSim("node-C", 3)}
	for i := 0; i < 40; i++ {
		for _, sim := range sims {
			s.son.Ingest(sim.Frame(0), now)
		}
	}
	s.demoRadios(now, rand.New(rand.NewSource(1))) // three nodes overhear three hosts
	v := s.finderView("192.168.1.20:51515")        // the "phone" asking is the host at .20
	b, _ := json.Marshal(v)
	var got struct {
		Hosts []struct {
			IP, Name, Nearest string
			Nodes             int
			Me                bool
			X, Y              float64
		}
		Me struct {
			Located bool
			X, Y    float64
		}
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Hosts) != 3 || !got.Me.Located {
		t.Fatalf("finder = %s", b)
	}
	for _, h := range got.Hosts {
		if h.Nodes != 3 || h.Nearest == "" || (h.Me != (h.IP == "192.168.1.20")) {
			t.Errorf("host %+v", h)
		}
		if h.Me && (h.X != got.Me.X || h.Y != got.Me.Y) {
			t.Errorf("the phone's own position %v,%v does not match its host entry %v,%v", got.Me.X, got.Me.Y, h.X, h.Y)
		}
	}
	if me := s.finderView("10.9.9.9:1")["me"].(map[string]any); me["located"] != false {
		t.Errorf("a phone the nodes cannot hear was given a position: %v", me)
	}
	// through the companion listener it needs the pairing code
	s.comp = companion{on: true, code: "424242", port: "7443"}
	w := httptest.NewRecorder()
	s.companionHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/finder", nil))
	if w.Code != 403 {
		t.Errorf("/finder without a code: %d", w.Code)
	}
}

func TestPhoneLinkFromTheConsole(t *testing.T) {
	s := demoServer(t)
	if r := call(t, s.handleMobile, `{"on":true}`); r["ok"] != false {
		t.Fatal("demo mode opened a port on the network from the console")
	}
	s.demo = false
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback networking here")
	}
	addr := ln.Addr().String()
	ln.Close()
	s.CompanionAddr = addr
	qr := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleMobileQR(w, httptest.NewRequest(http.MethodGet, "/api/mobile/qr.png", nil))
		return w
	}
	if qr().Code != 404 {
		t.Error("a QR code was served while the phone link was off")
	}
	r := call(t, s.handleMobile, `{"on":true}`)
	if r["ok"] != true || r["on"] != true || r["addr"] != addr || len(r["code"].(string)) != 6 {
		t.Fatalf("switch on: %v", r)
	}
	t.Cleanup(s.stopCompanion)
	if w := qr(); w.Code != 200 || !strings.HasPrefix(w.Body.String(), "\x89PNG") || w.Body.Len() < 300 {
		t.Errorf("QR code: %d, %d bytes", w.Code, w.Body.Len())
	}
	first := r["code"]
	if r = call(t, s.handleMobile, `{"newCode":true}`); r["code"] == first || r["on"] != true {
		t.Errorf("new code: %v (was %v)", r["code"], first)
	}
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("nothing is listening for phones on %s: %v", addr, err)
	}
	c.Close()
	if r = call(t, s.handleMobile, `{"on":false}`); r["on"] != false {
		t.Errorf("switch off: %v", r)
	}
	time.Sleep(100 * time.Millisecond)
	if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		c.Close()
		t.Error("the phone listener is still open after being switched off")
	}
}
