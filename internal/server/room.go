package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/r0n1n9/netwatch/internal/audit"
	"github.com/r0n1n9/netwatch/internal/braindance"
	"github.com/r0n1n9/netwatch/internal/drill"
	"github.com/r0n1n9/netwatch/internal/hostwatch"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/sonar"
)

// ---- the room: layout, walls, tripwires ----

func (s *Server) sonarLayout() map[string]sonar.Pt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]sonar.Pt, len(s.layout))
	for k, v := range s.layout {
		out[k] = sonar.Pt(v)
	}
	return out
}

type roomFile struct {
	Layout map[string]braindance.Point `json:"layout"`
	Walls  []sonar.Wire                `json:"walls"`
	Wires  []sonar.Wire                `json:"wires"`
}

func (s *Server) saveRoom() {
	if s.demo || s.DataDir == "" {
		return
	}
	s.mu.Lock()
	b, _ := json.Marshal(roomFile{Layout: s.layout, Walls: s.walls, Wires: s.wires})
	s.mu.Unlock()
	os.MkdirAll(s.DataDir, 0o700)
	os.WriteFile(filepath.Join(s.DataDir, "room.json"), b, 0o600)
}

func (s *Server) loadRoom() {
	b, err := os.ReadFile(filepath.Join(s.DataDir, "room.json"))
	if err != nil {
		return
	}
	var f roomFile
	if json.Unmarshal(b, &f) != nil {
		return
	}
	s.mu.Lock()
	if f.Layout != nil {
		s.layout = f.Layout
	}
	s.walls, s.wires = cleanWires(f.Walls), cleanWires(f.Wires)
	s.mu.Unlock()
}

func cleanWires(in []sonar.Wire) []sonar.Wire {
	out := []sonar.Wire{}
	clamp := func(v float64) float64 { return math.Max(-2, math.Min(2, v)) }
	for i, w := range in {
		if len(out) >= 60 {
			break
		}
		if math.Hypot(w.A.X-w.B.X, w.A.Y-w.B.Y) < 0.02 {
			continue // a click, not a line
		}
		if len(w.Name) > 40 {
			w.Name = w.Name[:40]
		}
		if w.ID == "" || len(w.ID) > 24 {
			w.ID = fmt.Sprintf("w%d-%d", time.Now().UnixNano()%1e9, i)
		}
		w.A, w.B = sonar.Pt{X: clamp(w.A.X), Y: clamp(w.A.Y)}, sonar.Pt{X: clamp(w.B.X), Y: clamp(w.B.Y)}
		out = append(out, w)
	}
	return out
}

func (s *Server) handleSonarMap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Walls []sonar.Wire `json:"walls"`
		Wires []sonar.Wire `json:"wires"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	s.walls, s.wires = cleanWires(req.Walls), cleanWires(req.Wires)
	s.mu.Unlock()
	s.saveRoom()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSonar(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.sonarView()) }

// sonarHost is a device on the network that the sensor nodes have placed in the room.
type sonarHost struct {
	MAC   string  `json:"mac"`
	IP    string  `json:"ip"`
	Name  string  `json:"name"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Nodes int     `json:"nodes"` // how many nodes hear it; 3 or more gives a real fix
	RSSI  int     `json:"rssi"`
}

// sonarView is the room as SONAR sees it: nodes, layout, located hosts, the presence estimate.
func (s *Server) sonarView() map[string]any {
	now := time.Now()
	nodes := s.son.Snapshot(now)
	lay := s.sonarLayout()

	type known struct{ ip, name string }
	s.mu.Lock()
	armed, listening := s.sonarArmed, s.fg.listening
	walls, wires := append([]sonar.Wire{}, s.walls...), append([]sonar.Wire{}, s.wires...)
	byMAC := map[string]known{}
	for _, d := range s.devices {
		name := d.Hostname
		if name == "" {
			name = d.IP.String()
		}
		byMAC[macStr(d.MAC)] = known{d.IP.String(), name}
	}
	s.mu.Unlock()

	hosts, unknown := []sonarHost{}, 0
	for _, rd := range s.son.Radios(now, nodes, lay) {
		if k, ok := byMAC[rd.MAC]; ok {
			hosts = append(hosts, sonarHost{rd.MAC, k.ip, k.name, rd.X, rd.Y, rd.Nodes, rd.RSSI})
		} else {
			unknown++
		}
	}
	est, estOK := sonar.Estimate(nodes, lay)
	return map[string]any{
		"armed": armed, "listening": listening, "demo": s.demo, "layout": lay, "nodes": nodes,
		"hosts": hosts, "unknownRadios": unknown, "people": sonar.People(nodes, lay),
		"walls": walls, "wires": wires, "est": map[string]any{"x": est.X, "y": est.Y, "ok": estOK},
	}
}

// ---- loops ----

// recordLoop samples the presence field five times a second into the
// braindance buffer. While SONAR is armed it also cuts a clip around each
// movement and raises an alert when the movement crosses a tripwire.
func (s *Server) recordLoop(ctx context.Context) {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	var autoStart, lastMotion time.Time
	var prev sonar.Pt
	havePrev := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			nodes := s.son.Snapshot(now)
			if len(nodes) == 0 {
				continue
			}
			sample := braindance.Sample{T: now.UnixMilli()}
			moving := false
			for _, n := range nodes {
				sample.Nodes = append(sample.Nodes, braindance.NodeSample{Node: n.Node, State: n.State, Score: n.Score, Breath: n.Breath, DistM: n.DistM})
				moving = moving || n.State == "motion"
			}
			s.bd.Add(sample)

			s.mu.Lock()
			armed := s.sonarArmed
			wires := append([]sonar.Wire(nil), s.wires...)
			s.mu.Unlock()

			est, ok := sonar.Estimate(nodes, s.sonarLayout())
			if ok && havePrev && armed {
				for _, w := range wires {
					if sonar.Crossed(prev, est, w) {
						s.tripped(w, now)
					}
				}
			}
			prev, havePrev = est, ok

			if armed && moving {
				if autoStart.IsZero() {
					autoStart = now.Add(-5 * time.Second)
				}
				lastMotion = now
			}
			if !autoStart.IsZero() && (now.Sub(lastMotion) > 5*time.Second || now.Sub(autoStart) > 9*time.Minute) {
				s.bd.Save("auto · "+autoStart.Add(5*time.Second).Format("15:04:05"), autoStart.UnixMilli(), now.UnixMilli(), s.layoutCopy(), true, s.wallSegs()...)
				autoStart = time.Time{}
			}
		}
	}
}

func (s *Server) tripped(w sonar.Wire, now time.Time) {
	s.mu.Lock()
	recent := now.Sub(s.wireHit[w.ID]) < 20*time.Second
	if !recent {
		s.wireHit[w.ID] = now
	}
	s.mu.Unlock()
	if recent {
		return
	}
	name := w.Name
	if name == "" {
		name = "unnamed"
	}
	s.emitAlert(model.Alert{Time: now, Severity: model.SevCritical, Kind: "tripwire", Source: "tripwire:" + w.ID,
		Message: "tripwire \"" + name + "\" crossed — movement passed from one side to the other"})
	s.fireScenes("tripwire", name, w.ID)
}

func (s *Server) onFall(node string) {
	s.mu.Lock()
	armed := s.sonarArmed
	s.mu.Unlock()
	if !armed {
		return // sitting down looks the same to this heuristic, so it only alerts while armed
	}
	s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevWarn, Kind: "fall", Source: node,
		Message: "possible fall on the " + node + " link — a sudden movement followed by stillness (experimental)"})
}

// minuteLoop appends one reading per node to the sleep log every minute.
func (s *Server) minuteLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.mu.Lock()
			moves := s.moves
			s.moves = map[string]int{}
			s.mu.Unlock()
			for _, n := range s.son.Snapshot(now) {
				if n.State == "offline" || n.State == "calibrating" {
					continue
				}
				s.sleep.Add(n.Node, sonar.SleepPoint{T: now.UnixMilli(), Breath: n.Breath, Moves: moves[n.Node]})
			}
		}
	}
}

// dailyLoop runs the surface diff and the detector fire drill shortly after
// start-up and then once a day, reporting each as an alert.
func (s *Server) dailyLoop(ctx context.Context) {
	wait := 8 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 24 * time.Hour

		lines := s.surfaceLines()
		if n := len(lines) - 1; n > 0 && !strings.HasPrefix(lines[1], "no changes") && !strings.HasPrefix(lines[1], "baseline") {
			s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevInfo, Kind: "surface", Source: "this host",
				Message: fmt.Sprintf("daily surface diff: %d change(s) — type \"surface diff\" to see them", n)})
		}

		rs := drill.Run()
		var failed []string
		for _, r := range rs {
			if !r.OK {
				failed = append(failed, r.Name)
			}
		}
		if len(failed) > 0 {
			s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevCritical, Kind: "drill", Source: "netwatch",
				Message: "fire drill: detectors did NOT respond — " + strings.Join(failed, ", ")})
		} else {
			s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevInfo, Kind: "drill", Source: "netwatch",
				Message: fmt.Sprintf("fire drill: all %d detectors responded to synthetic events", len(rs))})
		}
	}
}

// ---- sleep, lock, USB, surface, drill endpoints ----

func (s *Server) handleSleep(w http.ResponseWriter, r *http.Request) {
	nodes := s.sleep.Nodes()
	node := r.URL.Query().Get("node")
	if node == "" && len(nodes) > 0 {
		node = nodes[0]
	}
	series := s.sleep.Series(node)
	if series == nil {
		series = []sonar.SleepPoint{}
	}
	writeJSON(w, map[string]any{"nodes": nodes, "node": node, "series": series})
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if s.demo {
		writeJSON(w, map[string]any{"ok": true, "note": "demo mode: the screen was not locked"})
		return
	}
	if err := hostwatch.Lock(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) usbNow() []hostwatch.USBDev {
	if s.demo {
		return []hostwatch.USBDev{
			{Path: "1-1", VID: "046d", PID: "c52b", Vendor: "Logitech", Product: "USB Receiver", Keyboard: true},
			{Path: "1-4", VID: "0781", PID: "5581", Vendor: "SanDisk", Product: "Ultra"},
		}
	}
	return hostwatch.USBDevices(hostwatch.SysUSB)
}

func (s *Server) handleUSB(w http.ResponseWriter, r *http.Request) {
	devs := s.usbNow()
	if devs == nil {
		devs = []hostwatch.USBDev{}
	}
	writeJSON(w, map[string]any{"devices": devs})
}

func (s *Server) onUSB(d hostwatch.USBDev) {
	defer s.boardPluggedIn(d.VID)
	msg := "new USB device: " + d.Label()
	sev := model.SevInfo
	if d.Keyboard {
		msg += " — it presents a keyboard; make sure you plugged that in"
		sev = model.SevWarn
	}
	s.emitAlert(model.Alert{Time: time.Now(), Severity: sev, Kind: "usb", Source: "usb:" + d.Path, Message: msg})
}

func (s *Server) onFastKeys(d hostwatch.USBDev) {
	s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevCritical, Kind: "usb", Source: "usb:" + d.Path,
		Message: "keystroke injection suspected: " + d.Label() + " is typing faster than a person can — unplug it"})
}

func (s *Server) surfaceNow() hostwatch.Surface {
	cur := hostwatch.Surface{Taken: time.Now()}
	if s.demo {
		cur.Listeners = []string{"tcp   0.0.0.0:22", "tcp   127.0.0.1:631", "tcp   0.0.0.0:8384"}
		cur.Persistence = []string{"[systemd  ] /etc/systemd/system/syncthing.service", "[shell-rc ] /home/ronin/.bashrc"}
	} else {
		cur.Listeners = audit.Listeners()[1:]
		cur.Persistence = audit.Persistence()[1:]
	}
	s.mu.Lock()
	for _, d := range s.devices {
		cur.Hosts = append(cur.Hosts, strings.TrimSpace(macStr(d.MAC)+" "+d.IP.String()+" "+d.Hostname))
	}
	s.mu.Unlock()
	for _, d := range s.usbNow() {
		cur.USB = append(cur.USB, d.Label())
	}
	return cur
}

func (s *Server) surfaceLines() []string {
	cur := s.surfaceNow()
	base, since, ok := s.surface.Baseline(cur)
	if !ok {
		return []string{"◆ SURFACE DIFF", "baseline could not be stored"}
	}
	diff := hostwatch.Diff(base, cur)
	head := fmt.Sprintf("◆ SURFACE DIFF · %d change(s) since %s · listeners, start-up entries, hosts, USB", len(diff), since)
	if len(diff) == 0 {
		return []string{head, "no changes"}
	}
	return append([]string{head}, diff...)
}

func (s *Server) handleSurface(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "lines": s.surfaceLines()})
}

func (s *Server) handleDrill(w http.ResponseWriter, r *http.Request) {
	rs := drill.Run()
	writeJSON(w, map[string]any{"ok": true, "results": rs, "lines": drill.Lines(rs)})
}

// ---- demo scene ----

// demoRoom gives the demo a floor plan, a tripwire, located hosts, a night of
// breathing data and a yesterday to diff against.
func (s *Server) demoRoom() {
	seg := func(id, name string, x1, y1, x2, y2 float64) sonar.Wire {
		return sonar.Wire{ID: id, Name: name, A: sonar.Pt{X: x1, Y: y1}, B: sonar.Pt{X: x2, Y: y2}}
	}
	s.mu.Lock()
	s.layout = map[string]braindance.Point{"@ap": {X: 0, Y: -0.1}, "node-A": {X: -0.75, Y: -0.55}, "node-B": {X: 0.1, Y: 0.85}, "node-C": {X: 0.8, Y: -0.3}}
	s.walls = []sonar.Wire{
		seg("wall-n", "", -1.15, -0.84, 1.15, -0.84), seg("wall-e", "", 1.15, -0.84, 1.15, 1.05),
		seg("wall-s", "", 1.15, 1.05, -1.15, 1.05), seg("wall-w", "", -1.15, 1.05, -1.15, -0.84),
		seg("wall-i", "", -1.15, 0.3, -0.45, 0.3),
	}
	s.wires = []sonar.Wire{seg("wire-hall", "hallway", -0.6, 0.12, 0.4, 0.12)}
	s.mu.Unlock()

	rng := rand.New(rand.NewSource(11))
	now := time.Now()
	for m := 480; m > 0; m-- { // eight hours on the node-C link
		p := sonar.SleepPoint{T: now.Add(-time.Duration(m) * time.Minute).UnixMilli(), Breath: math.Round(14.5 + 1.5*math.Sin(float64(m)/55) + rng.NormFloat64()*0.5)}
		if rng.Float64() < 0.07 {
			p.Moves, p.Breath = 1+rng.Intn(3), 0 // turning over
		}
		s.sleep.Add("node-C", p)
	}

	y := s.surfaceNow()
	y.Taken = now.Add(-24 * time.Hour)
	y.Listeners = y.Listeners[:2]                                          // port 8384 is new today
	y.Hosts = append(y.Hosts, "aa:bb:cc:10:20:30 192.168.1.77 old-tablet") // gone today
	s.surface.Baseline(y)
}

// demoRadios makes the demo nodes overhear three network hosts so they appear on the map.
func (s *Server) demoRadios(now time.Time, rng *rand.Rand) {
	lay := s.sonarLayout()
	nodes := s.son.Snapshot(now)
	t := float64(now.UnixMilli()) / 1000
	hosts := map[string]sonar.Pt{
		"b8:27:eb:11:22:33": {X: 0.6, Y: -0.65},                                        // pi-hole
		"dc:3a:5e:00:11:22": {X: -0.7, Y: 0.6},                                         // echo-dot
		"02:11:22:33:44:55": {X: 0.25 + 0.3*math.Cos(t/9), Y: 0.5 + 0.2*math.Sin(t/9)}, // the unverified host, wandering
	}
	for _, n := range nodes {
		np, ok := sonar.NodePos(n, lay)
		if !ok {
			continue
		}
		var seen []sonar.Sighting
		for mac, hp := range hosts {
			d := math.Max(0.3, math.Hypot(hp.X-np.X, hp.Y-np.Y)*sonar.MetresPerUnit)
			seen = append(seen, sonar.Sighting{MAC: mac, RSSI: int(math.Round(-40 - 25*math.Log10(d) + rng.NormFloat64()*1.5))})
		}
		s.son.Ingest(sonar.Frame{Node: n.Node, Seen: seen}, now)
	}
}
