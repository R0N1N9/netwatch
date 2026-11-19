package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r0n1n9/netwatch/internal/pulse"
)

func getJSON(t *testing.T, h http.HandlerFunc, url string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, url, nil))
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad response %q: %v", w.Body.String(), err)
	}
	return out
}

func demoServer(t *testing.T) *Server {
	s := testServer(t)
	s.loadDemo()
	s.demoModules()
	return s
}

func TestDemoDecoysTouchNothing(t *testing.T) {
	s := demoServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, body := range []string{`{"action":"port-open","port":8022}`, `{"action":"file-plant","name":"keys.txt"}`, `{"action":"name-add","name":"vault.test.internal"}`} {
		if r := call(t, s.handleDecoy, body); r["ok"] != true {
			t.Fatalf("%s: %v", body, r)
		}
	}
	st := getJSON(t, s.handleDecoy, "/api/decoy")["state"].(map[string]any)
	if len(st["ports"].([]any)) != 3 || len(st["files"].([]any)) != 2 || len(st["names"].([]any)) != 2 {
		t.Errorf("demo decoy state = %v", st)
	}
	if real := s.mod.dec.State(); len(real.Ports)+len(real.Files)+len(real.Names) != 0 {
		t.Errorf("demo mode planted real decoys: %+v", real)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("demo mode wrote into the home directory: %v", entries)
	}
	before := len(s.alerts)
	call(t, s.handleDecoy, `{"action":"test"}`)
	if len(s.alerts) != before+1 || s.alerts[0].Kind != "decoy-drill" || s.alerts[0].Severity.String() != "INFO" {
		t.Errorf("the drill did not raise an informational drill alert: %+v", s.alerts[0])
	}
	if r := call(t, s.handleDecoy, `{"action":"port-open","port":70000}`); r["ok"] != false {
		t.Error("accepted port 70000")
	}
}

func TestRealDecoysTripAndStayInsideHome(t *testing.T) {
	s := testServer(t)
	s.demo = false
	home := t.TempDir()
	t.Setenv("HOME", home)
	if r := call(t, s.handleDecoy, `{"action":"file-plant","dir":"/etc","name":"x.txt"}`); r["ok"] != false {
		t.Fatal("planted a canary outside the home directory")
	}
	if r := call(t, s.handleDecoy, `{"action":"file-plant","dir":"`+home+`","name":"passwords.txt"}`); r["ok"] != true {
		t.Fatalf("plant: %v", r)
	}
	t.Cleanup(s.mod.dec.Close)
	time.Sleep(400 * time.Millisecond) // the watcher ignores the first moments after planting
	if r := call(t, s.handleDecoy, `{"action":"test"}`); r["ok"] != true {
		t.Fatalf("drill: %v", r)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.alerts)
		s.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.alerts) == 0 || s.alerts[0].Kind != "decoy-drill" || !strings.Contains(s.alerts[0].Message, filepath.Join(home, "passwords.txt")) {
		t.Errorf("opening the canary raised no alert: %+v", s.alerts)
	}
}

func TestLedgerRecordsAlertsAndActions(t *testing.T) {
	s := demoServer(t)
	l := getJSON(t, s.handleLedger, "/api/ledger")
	n := int(l["len"].(float64))
	if l["intact"] != true || n < 6 {
		t.Fatalf("demo ledger = len %d intact %v", n, l["intact"])
	}
	call(t, s.handleTrust, `{"ip":"192.168.1.31","trusted":true}`)
	call(t, s.handleBlackwall, `{"ip":"192.168.1.99","action":"quarantine"}`)
	call(t, s.handleLedgerNote, `{"text":"called the landlord"}`)
	l = getJSON(t, s.handleLedger, "/api/ledger")
	if int(l["len"].(float64)) != n+3 || l["intact"] != true {
		t.Errorf("ledger after three actions: len %v intact %v", l["len"], l["intact"])
	}
	top := l["entries"].([]any)[0].(map[string]any)
	if top["kind"] != "note" || top["text"] != "called the landlord" {
		t.Errorf("newest entry = %v", top)
	}
	if !s.quarantined["192.168.1.99"] {
		t.Error("demo quarantine was not reflected in the console state")
	}
	rep := getJSON(t, s.handleLedgerReport, "/api/ledger/report")["report"].(string)
	if !strings.Contains(rep, "INCIDENT REPORT") || !strings.Contains(rep, "192.168.1.99") {
		t.Errorf("report = %q", rep)
	}
}

func TestCortexQueueAndCase(t *testing.T) {
	s := demoServer(t)
	q := getJSON(t, s.handleCortex, "/api/cortex")["queue"].([]any)
	if len(q) != len(s.alerts) {
		t.Fatalf("queue has %d rows for %d alerts", len(q), len(s.alerts))
	}
	verdicts := map[string]string{}
	for _, r := range q {
		row := r.(map[string]any)
		verdicts[row["kind"].(string)+"@"+row["source"].(string)] = row["verdict"].(string) + "/" + row["action"].(string)
	}
	if verdicts["decoy@192.168.1.99"] != "likely threat/quarantine" || verdicts["arp-spoof@192.168.1.1"] != "needs a look/verify" || verdicts["new-device@192.168.1.31"] == "likely threat/quarantine" {
		t.Errorf("verdicts = %v", verdicts)
	}
	var decoyTS int64
	for _, a := range s.alerts {
		if a.Kind == "decoy" {
			decoyTS = a.Time.UnixMilli()
		}
	}
	b, _ := json.Marshal(map[string]any{"ts": decoyTS, "source": "192.168.1.99", "kind": "decoy", "narrate": true})
	r := call(t, s.handleCortex, string(b))
	c := r["case"].(map[string]any)
	if r["ok"] != true || len(c["steps"].([]any)) < 4 || len(c["for"].([]any)) < 3 || len(c["against"].([]any)) < 1 {
		t.Errorf("case = %v", r)
	}
	if _, has := r["narrative"]; !has || r["narrative"] != "" {
		t.Errorf("narrative without a local model = %v", r["narrative"])
	}
	if r := call(t, s.handleCortex, `{"ts":1,"source":"x","kind":"y"}`); r["ok"] != false {
		t.Error("investigated an alert that does not exist")
	}
}

func TestOracleWardQuantumSpectrum(t *testing.T) {
	s := demoServer(t)
	reach := getJSON(t, s.handleOracle, "/api/oracle?host=192.168.1.99")["reach"].(map[string]any)
	if len(reach["internal"].([]any)) != 5 || len(reach["external"].([]any)) != 2 || reach["toThis"].(float64) != 2 {
		t.Errorf("reach of the unverified host = %v", reach)
	}
	w := call(t, s.handleWhatIf, `{"rule":"port 23"}`)
	if w["ok"] != true || len(w["result"].(map[string]any)["matches"].([]any)) != 1 {
		t.Errorf("what-if port 23 = %v", w)
	}
	if w := call(t, s.handleWhatIf, `{"rule":"make it so"}`); w["ok"] != false {
		t.Error("accepted a rule that makes no sense")
	}

	wd := getJSON(t, s.handleWard, "/api/ward")
	devs := wd["deviations"].([]any)
	if len(wd["profiles"].([]any)) != 4 || len(devs) != 1 {
		t.Fatalf("ward: %d profiles, %d deviations", len(wd["profiles"].([]any)), len(devs))
	}
	seen, _ := json.Marshal(devs[0].(map[string]any)["seen"])
	if r := call(t, s.handleWard, `{"action":"accept","seen":`+string(seen)+`}`); r["ok"] != true {
		t.Fatalf("accept: %v", r)
	}
	if n := len(getJSON(t, s.handleWard, "/api/ward")["deviations"].([]any)); n != 0 {
		t.Errorf("%d deviations left after accepting", n)
	}

	qv := getJSON(t, s.handleQuantum, "/api/quantum")
	if len(qv["findings"].([]any)) != 6 || qv["pq"].(float64) != 2 {
		t.Errorf("quantum demo = %v findings, %v post-quantum", len(qv["findings"].([]any)), qv["pq"])
	}

	// SPECTRUM through the same door the sensor nodes use
	frame := `{"node":"node-A","seen":[{"mac":"b8:27:eb:11:22:33","rssi":-50},{"mac":"de:ad:be:ef:00:01","rssi":-66}],"ble":[{"addr":"c1:11:11:11:11:11","rssi":-70,"company":76,"type":18}],"aps":[{"ssid":"home","bssid":"f0:27:2d:aa:bb:cc","rssi":-40}],"deauth":25}`
	rec := httptest.NewRecorder()
	s.handleSonarIngest(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(frame)))
	sp := getJSON(t, s.handleSpectrum, "/api/spectrum")
	tags := map[string]string{}
	for _, r := range sp["radios"].([]any) {
		row := r.(map[string]any)
		tags[row["id"].(string)] = row["tag"].(string) + ":" + row["name"].(string)
	}
	if tags["b8:27:eb:11:22:33"] != "network host:pi-hole.lan" || tags["de:ad:be:ef:00:01"] != "unknown radio:" || tags["c1:11:11:11:11:11"] != "tracker?:" {
		t.Errorf("radio tags = %v", tags)
	}
	if len(sp["aps"].([]any)) != 1 || sp["deauth"].(float64) != 25 {
		t.Errorf("aps %v deauth %v", sp["aps"], sp["deauth"])
	}
	if s.alerts[0].Kind != "spectrum" || !strings.Contains(s.alerts[0].Message, "disconnect") {
		t.Errorf("no deauth alert: %+v", s.alerts[0])
	}
	if r := call(t, s.handleSpectrum, `{"ssids":["home"]}`); r["ok"] != true || r["trusted"].(float64) != 1 {
		t.Errorf("trust: %v", r)
	}
	call(t, s.handleSpectrum, `{"radio":"de:ad:be:ef:00:01","mine":true}`)
	for _, r := range getJSON(t, s.handleSpectrum, "/api/spectrum")["radios"].([]any) {
		if row := r.(map[string]any); row["id"] == "de:ad:be:ef:00:01" && row["tag"] != "mine" {
			t.Errorf("claimed radio is tagged %v", row["tag"])
		}
	}
}

func TestPulseEnrollsThenFlagsAStranger(t *testing.T) {
	s := demoServer(t)
	strokes := func(dwell, flight float64, n int) string {
		v := make([]pulse.Stroke, n)
		for i := range v {
			j := float64(i%7) - 3
			v[i] = pulse.Stroke{Dwell: dwell + 4*j, Flight: flight + 9*j}
		}
		b, _ := json.Marshal(map[string]any{"strokes": v})
		return string(b)
	}
	r := call(t, s.handlePulse, strokes(95, 140, pulse.EnrollN+5))
	if st := r["state"].(map[string]any); st["enrolled"] != true {
		t.Fatalf("not enrolled after %d strokes: %v", pulse.EnrollN+5, st)
	}
	r = call(t, s.handlePulse, strokes(95, 140, pulse.WindowN))
	if st := r["state"].(map[string]any); st["match"].(float64) < 70 || st["mismatch"] == true {
		t.Errorf("the owner does not match their own rhythm: %v", st)
	}
	before := len(s.alerts)
	call(t, s.handlePulse, strokes(160, 320, pulse.WindowN*2))
	if len(s.alerts) != before+1 || s.alerts[0].Kind != "pulse" {
		t.Errorf("a different rhythm raised no alert (alerts %d → %d)", before, len(s.alerts))
	}
	if r := call(t, s.handlePulse, `{"reset":true}`); r["state"].(map[string]any)["enrolled"] != false {
		t.Errorf("reset did not forget the profile: %v", r)
	}
}

func TestModulesSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	s := testServer(t)
	s.demo, s.DataDir = false, dir
	s.mod.dec.AddName("vault.test.internal")
	s.handleSonarIngest(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"node":"n","aps":[{"ssid":"home","bssid":"f0:27:2d:aa:bb:cc","rssi":-40}]}`)))
	call(t, s.handleSpectrum, `{"ssids":["home"]}`)
	call(t, s.handleSpectrum, `{"radio":"de:ad:be:ef:00:01","mine":true}`)
	s.saveModules()

	s2 := testServer(t)
	s2.demo, s2.DataDir = false, dir
	s2.loadModules()
	if st := s2.mod.dec.State(); len(st.Names) != 1 || st.Names[0] != "vault.test.internal" {
		t.Errorf("canary names after restart = %v", st.Names)
	}
	if mine := s2.mod.spec.Mine(); len(mine) != 1 || mine[0] != "de:ad:be:ef:00:01" {
		t.Errorf("claimed radios after restart = %v", mine)
	}
	if _, ssids := s2.mod.spec.Trusted(); len(ssids) != 1 || ssids[0] != "home" {
		t.Errorf("approved networks after restart = %v", ssids)
	}
}

func TestHoneyportTouchIsCriticalButDrillIsNot(t *testing.T) {
	s := testServer(t)
	s.demo = false
	t.Setenv("HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0") // find a free port
	if err != nil {
		t.Skip("no loopback networking here")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if r := call(t, s.handleDecoy, fmt.Sprintf(`{"action":"port-open","port":%d}`, port)); r["ok"] != true {
		t.Fatalf("open: %v", r)
	}
	t.Cleanup(s.mod.dec.Close)
	touch := func() {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(c, "GET / HTTP/1.0\r\n\r\n")
		c.Close()
	}
	newest := func(want int) (kind, sev string) {
		for end := time.Now().Add(4 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			s.mu.Lock()
			if len(s.alerts) >= want {
				kind, sev = s.alerts[0].Kind, s.alerts[0].Severity.String()
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
		}
		return "", ""
	}
	touch() // not a drill: something on this machine connected to the decoy
	if kind, sev := newest(1); kind != "decoy" || sev != "CRITICAL" {
		t.Errorf("an unannounced touch raised %q/%q, want decoy/CRITICAL", kind, sev)
	}
	call(t, s.handleDecoy, `{"action":"test"}`)
	if kind, sev := newest(2); kind != "decoy-drill" || sev != "INFO" {
		t.Errorf("the drill raised %q/%q, want decoy-drill/INFO", kind, sev)
	}
}

func TestCrossSiteRequestsAreRefused(t *testing.T) {
	s := demoServer(t)
	try := func(origin, site string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/decoy", strings.NewReader(`{"action":"port-open","port":8022}`))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		s.handleDecoy(w, r)
		return w.Code
	}
	if c := try("http://127.0.0.1:8080", "same-origin"); c != http.StatusOK {
		t.Errorf("the console's own request got %d", c)
	}
	if c := try("", ""); c != http.StatusOK {
		t.Errorf("a request with no origin (curl, a sensor node) got %d", c)
	}
	for _, o := range [][2]string{{"https://evil.example", "cross-site"}, {"https://evil.example", ""}, {"http://127.0.0.1:9999", ""}, {"", "cross-site"}} {
		if c := try(o[0], o[1]); c != http.StatusForbidden {
			t.Errorf("origin %q / site %q got %d, want 403", o[0], o[1], c)
		}
	}
	if n := len(s.mod.demoDecoys.Ports); n != 3 {
		t.Errorf("%d demo decoy ports after two allowed requests for the same port and four refused ones, want 2 + 1", n)
	}
}
