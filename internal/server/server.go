// Package server hosts the NetWatch GUI: it serves the embedded web console and
// streams live network state to it over Server-Sent Events, while exposing
// action endpoints (sweep, trust, BLACKWALL quarantine, offensive scan, copilot).
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/r0n1n9/netwatch/internal/audit"
	"github.com/r0n1n9/netwatch/internal/blackwall"
	"github.com/r0n1n9/netwatch/internal/braindance"
	"github.com/r0n1n9/netwatch/internal/copilot"
	"github.com/r0n1n9/netwatch/internal/drill"
	"github.com/r0n1n9/netwatch/internal/geo"
	"github.com/r0n1n9/netwatch/internal/hostwatch"
	"github.com/r0n1n9/netwatch/internal/ice"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/netif"
	"github.com/r0n1n9/netwatch/internal/offensive"
	"github.com/r0n1n9/netwatch/internal/passive"
	"github.com/r0n1n9/netwatch/internal/sonar"
	"github.com/r0n1n9/netwatch/internal/sweep"
	"github.com/r0n1n9/netwatch/internal/sysmon"
	"github.com/r0n1n9/netwatch/internal/webui"
)

// Server is the NetWatch backend.
type Server struct {
	info      *netif.Info
	engine    *ice.Engine
	wall      *blackwall.Wall
	cop       copilot.Copilot
	mon       *passive.Monitor
	sys       sysmon.Monitor
	autoEvery time.Duration
	demo      bool

	mu          sync.Mutex
	devices     []model.Device
	alerts      []model.Alert
	trusted     map[string]bool
	quarantined map[string]bool
	sweeping    bool
	lastSweep   time.Time

	clientsMu sync.Mutex
	clients   map[chan []byte]struct{}

	// state history for the time scrubber
	histMu sync.Mutex
	hist   []histEntry

	// SONAR: CSI presence sensing
	son          *sonar.Engine
	SonarAddr    string // UDP address to ingest CSI frames on ("" = off)
	sonarArmed   bool
	lastPresence map[string]time.Time
	layout       map[string]braindance.Point // room map positions, shared by every browser

	// braindance: recordings of the presence field
	bd      *braindance.Recorder
	DataDir string // where clips are kept ("" = memory only)

	// presence gate: sensitive controls need the key device on the network
	gateMAC  string
	gateName string

	// room: floor plan walls and tripwires drawn on the SONAR map
	walls   []sonar.Wire
	wires   []sonar.Wire
	wireHit map[string]time.Time

	sleep   *sonar.SleepLog
	moves   map[string]int // movement onsets this minute, per node
	surface hostwatch.SurfaceStore

	mod modules // the module decks (modules.go)
	sen senses  // rehearsal, recap, operator watch, acknowledgements (senses.go)
	ref reflex  // scenes, gestures, light, talking decoy (reflex.go)

	CompanionAddr string // HTTPS address for the phone companion ("" = off)
	fg            forgeState
	wantListen    bool // listen for nodes at start-up, remembered from an earlier run
	comp          companion
}

type histEntry struct {
	TS     int64 `json:"ts"`
	Hosts  int   `json:"hosts"`
	Alerts int   `json:"alerts"`
	Crit   int   `json:"crit"`
	snap   []byte
}

// New builds a server for an interface.
func New(info *netif.Info, autoEvery time.Duration, demo bool) *Server {
	s := &Server{
		info:         info,
		engine:       ice.New(),
		wall:         blackwall.New(),
		cop:          copilot.Detect(context.Background()),
		son:          sonar.New(),
		lastPresence: map[string]time.Time{},
		layout:       map[string]braindance.Point{},
		bd:           braindance.New(""),
		wireHit:      map[string]time.Time{},
		moves:        map[string]int{},
		sleep:        sonar.OpenSleepLog(""),
		autoEvery:    autoEvery,
		demo:         demo,
		trusted:      map[string]bool{},
		quarantined:  map[string]bool{},
		clients:      map[chan []byte]struct{}{},
	}
	s.mon = passive.NewMonitor(info.Iface, info.IP, s.emitAlert)
	s.son.OnMotion = s.onPresence
	s.son.OnFall = s.onFall
	s.initModules()
	s.initSenses()
	s.initReflex()
	return s
}

// CopilotName reports which Copilot backend is answering.
func (s *Server) CopilotName() string { return s.cop.Name() }

// emitAlert records an alert (from the passive monitor) and pushes to clients.
func (s *Server) emitAlert(a model.Alert) {
	s.logAlert(a)
	s.mu.Lock()
	s.alerts = append([]model.Alert{a}, s.alerts...)
	if len(s.alerts) > 200 {
		s.alerts = s.alerts[:200]
	}
	s.mu.Unlock()
	s.afterAlert(a)
	s.broadcast()
}

// Handler returns the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(webui.FS())))
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/api/sweep", s.handleSweep)
	mux.HandleFunc("/api/trust", s.handleTrust)
	mux.HandleFunc("/api/blackwall", s.handleBlackwall)
	mux.HandleFunc("/api/scan", s.handleScan)
	mux.HandleFunc("/api/copilot", s.handleCopilot)
	// kill-chain blue-team audits (read-only, localhost)
	mux.HandleFunc("/api/kc/listeners", auditHandler(audit.Listeners))
	mux.HandleFunc("/api/kc/persistence", auditHandler(audit.Persistence))
	mux.HandleFunc("/api/kc/connections", auditHandler(audit.Connections))
	mux.HandleFunc("/api/kc/eicar", auditHandler(audit.Eicar))
	mux.HandleFunc("/api/globe", s.handleGlobe)
	mux.HandleFunc("/api/sys", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.sys.Snapshot()) })
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/api/history/at", s.handleHistoryAt)
	mux.HandleFunc("/api/sonar", s.handleSonar)
	mux.HandleFunc("/api/sonar/ingest", s.handleSonarIngest)
	mux.HandleFunc("/api/sonar/arm", s.handleSonarArm)
	mux.HandleFunc("/api/sonar/layout", s.handleSonarLayout)
	mux.HandleFunc("/api/braindance", s.handleBD)
	mux.HandleFunc("/api/braindance/save", s.handleBDSave)
	mux.HandleFunc("/api/braindance/rec", s.handleBDRec)
	mux.HandleFunc("/api/braindance/clip", s.handleBDClip)
	mux.HandleFunc("/api/braindance/delete", s.handleBDDelete)
	mux.HandleFunc("/api/lineage", s.handleLineage)
	mux.HandleFunc("/api/kc/lineage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "lines": s.lineageLines()})
	})
	mux.HandleFunc("/api/gate", s.handleGate)
	mux.HandleFunc("/api/sonar/map", s.handleSonarMap)
	mux.HandleFunc("/api/sonar/sleep", s.handleSleep)
	mux.HandleFunc("/api/lock", s.handleLock)
	mux.HandleFunc("/api/usb", s.handleUSB)
	mux.HandleFunc("/api/surface", s.handleSurface)
	mux.HandleFunc("/api/drill", s.handleDrill)
	mux.HandleFunc("/api/kc/surface", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "lines": s.surfaceLines()})
	})
	mux.HandleFunc("/api/kc/drill", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "lines": drill.Lines(drill.Run())})
	})
	s.moduleRoutes(mux)
	s.senseRoutes(mux)
	s.reflexRoutes(mux)
	mux.HandleFunc("/api/forge", s.handleForge)
	mux.HandleFunc("/api/finder", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.finderView(r.RemoteAddr)) })
	return mux
}

// Run starts the background watch loop and the HTTP server.
func (s *Server) Run(ctx context.Context, addr string) error {
	s.mu.Lock()
	s.fg.ctx = ctx
	s.mu.Unlock()
	go s.sys.Run(ctx)
	if !s.demo && s.DataDir != "" {
		s.bd = braindance.New(filepath.Join(s.DataDir, "braindance"))
		s.sleep = sonar.OpenSleepLog(filepath.Join(s.DataDir, "sleep.json"))
		s.surface.Dir = filepath.Join(s.DataDir, "surface")
		s.loadRoom()
	}
	if !s.demo {
		go hostwatch.WatchUSB(ctx, hostwatch.SysUSB, s.onUSB, s.onFastKeys)
	}
	go s.recordLoop(ctx)
	go s.minuteLoop(ctx)
	go s.dailyLoop(ctx)
	if s.demo {
		s.loadDemo()
		s.demoRoom() // after the sample hosts exist, so yesterday's snapshot includes them
	}
	s.startModules(ctx)
	go s.sensesLoop(ctx)
	s.startReflex(ctx)
	if s.demo {
		go s.sonarDemo(ctx)
	} else {
		if s.SonarAddr != "" {
			s.listenForNodes(s.SonarAddr)
		} else if s.wantListen {
			s.listenForNodes(sonarDefaultListen) // a board was flashed from the console earlier
		}
		go s.watchLoop(ctx)
		go func() {
			if err := s.mon.Run(ctx); err != nil {
				log.Printf("passive monitor: %v (OS/JA3/anomaly/globe disabled — needs cap_net_raw)", err)
			}
		}()
	}
	srv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// ---- background watch ----

func (s *Server) watchLoop(ctx context.Context) {
	s.doSweep(ctx)
	if s.autoEvery <= 0 {
		return
	}
	t := time.NewTicker(s.autoEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.doSweep(ctx)
		}
	}
}

func (s *Server) doSweep(ctx context.Context) {
	s.mu.Lock()
	if s.sweeping {
		s.mu.Unlock()
		return
	}
	s.sweeping = true
	trusted := cloneSet(s.trusted)
	s.mu.Unlock()
	s.broadcast()

	opt := sweep.Default()
	opt.Duration = 2500 * time.Millisecond
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	devices, err := sweep.Run(c, s.info, opt)
	cancel()

	var newAlerts []model.Alert
	s.mu.Lock()
	s.sweeping = false
	s.lastSweep = time.Now()
	if err == nil {
		for i := range devices {
			devices[i].Trusted = s.trusted[devices[i].IP.String()]
		}
		newAlerts = s.engine.Observe(devices, trusted)
		s.devices = s.withRehearsalHost(devices)
		for _, a := range newAlerts {
			s.logAlert(a)
			s.alerts = append([]model.Alert{a}, s.alerts...)
		}
		if len(s.alerts) > 200 {
			s.alerts = s.alerts[:200]
		}
	}
	s.mu.Unlock()
	for _, a := range newAlerts {
		s.afterAlert(a)
	}
	s.broadcast()
}

// ---- SSE ----

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 8)
	s.clientsMu.Lock()
	s.clients[ch] = struct{}{}
	s.clientsMu.Unlock()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, ch)
		s.clientsMu.Unlock()
	}()

	// Immediate snapshot.
	fmt.Fprintf(w, "data: %s\n\n", s.snapshotJSON())
	fl.Flush()

	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-keep.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		}
	}
}

func (s *Server) broadcast() {
	b := s.snapshotJSON()
	s.record(b, time.Now())
	s.clientsMu.Lock()
	for ch := range s.clients {
		select {
		case ch <- b:
		default: // drop for slow clients
		}
	}
	s.clientsMu.Unlock()
}

// ---- action endpoints ----

func (s *Server) handleSweep(w http.ResponseWriter, r *http.Request) {
	if s.demo {
		// No real interface in demo mode: show the scan state, keep sample data.
		go func() {
			s.mu.Lock()
			s.sweeping = true
			s.mu.Unlock()
			s.broadcast()
			time.Sleep(1500 * time.Millisecond)
			s.mu.Lock()
			s.sweeping = false
			s.lastSweep = time.Now()
			s.mu.Unlock()
			s.broadcast()
		}()
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	go s.doSweep(context.Background())
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleTrust(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP      string `json:"ip"`
		Trusted bool   `json:"trusted"`
	}
	if !decode(w, r, &req) {
		return
	}
	if msg := s.gateRefusal(); msg != "" {
		writeJSON(w, map[string]any{"ok": false, "error": msg})
		return
	}
	s.mu.Lock()
	s.trusted[req.IP] = req.Trusted
	for i := range s.devices {
		if s.devices[i].IP.String() == req.IP {
			s.devices[i].Trusted = req.Trusted
		}
	}
	s.mu.Unlock()
	if req.Trusted {
		s.logAction(req.IP, "marked "+req.IP+" trusted")
	} else {
		s.logAction(req.IP, "removed trust from "+req.IP)
	}
	s.broadcast()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleBlackwall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP     string `json:"ip"`
		Action string `json:"action"`
	}
	if !decode(w, r, &req) {
		return
	}
	if net.ParseIP(req.IP) == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid ip"})
		return
	}
	if msg := s.gateRefusal(); msg != "" {
		writeJSON(w, map[string]any{"ok": false, "error": msg})
		return
	}
	if s.rehearsalBlackwall(w, req.IP, req.Action) {
		return
	}
	var err error
	switch req.Action {
	case "quarantine":
		if !s.demo { // the demo never touches the firewall
			err = s.wall.Quarantine(req.IP)
		}
		if err == nil {
			a := model.Alert{Time: time.Now(), Severity: model.SevWarn, Kind: "blackwall",
				Source: req.IP, Message: "BLACKWALL engaged — " + req.IP + " quarantined at the firewall"}
			s.mu.Lock()
			s.quarantined[req.IP] = true
			s.alerts = append([]model.Alert{a}, s.alerts...)
			s.mu.Unlock()
			s.logAction(req.IP, "quarantined "+req.IP+" at the firewall")
		}
	case "release":
		if !s.demo {
			err = s.wall.Release(req.IP)
		}
		if err == nil {
			s.mu.Lock()
			delete(s.quarantined, req.IP)
			s.mu.Unlock()
			s.logAction(req.IP, "released "+req.IP+" from quarantine")
		}
	default:
		err = fmt.Errorf("unknown action")
	}
	s.broadcast()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if !decode(w, r, &req) {
		return
	}
	lines := offensive.PortScan(r.Context(), req.Target)
	writeJSON(w, map[string]any{"ok": true, "lines": lines})
}

func (s *Server) handleCopilot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	snap := copilot.Context{
		Devices:     append([]model.Device(nil), s.devices...),
		Alerts:      append([]model.Alert(nil), s.alerts...),
		Quarantined: cloneSet(s.quarantined),
		Intel:       map[string]copilot.HostIntel{},
		Iface:       s.info.Iface.Name,
		Subnet:      s.info.IPNet.String(),
	}
	for i := range snap.Devices {
		ip := snap.Devices[i].IP.String()
		snap.Devices[i].Trusted = s.trusted[ip]
		e := s.enrich(ip)
		snap.Intel[ip] = copilot.HostIntel{OS: e.OS, Anomaly: e.Anomaly, Beacon: e.Beacon}
	}
	s.mu.Unlock()
	answer := s.cop.Ask(r.Context(), req.Query, snap)
	writeJSON(w, map[string]any{"answer": answer, "backend": s.cop.Name()})
}

// ---- snapshot ----

type wireDevice struct {
	IP          string  `json:"ip"`
	MAC         string  `json:"mac"`
	Vendor      string  `json:"vendor"`
	Hostname    string  `json:"hostname"`
	Self        bool    `json:"self"`
	Gateway     bool    `json:"gateway"`
	Trusted     bool    `json:"trusted"`
	Quarantined bool    `json:"quarantined"`
	OS          string  `json:"os"`
	JA3         string  `json:"ja3"`
	SNI         string  `json:"sni"`
	Anomaly     float64 `json:"anomaly"`
	Beacon      string  `json:"beacon"`
}

type wireAlert struct {
	TS       int64  `json:"ts"`
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Message  string `json:"message"`
	Acked    bool   `json:"acked"`          // the operator has opened or dismissed it
	Clip     string `json:"clip,omitempty"` // braindance clip of the room when it happened
}

func alertKey(a model.Alert) string {
	return fmt.Sprintf("%d|%s|%s", a.Time.UnixMilli(), a.Source, a.Kind)
}

func wireOf(a model.Alert, acked bool) wireAlert {
	return wireAlert{TS: a.Time.UnixMilli(), Severity: a.Severity.String(), Kind: a.Kind, Source: a.Source, Message: a.Message, Acked: acked}
}

func (s *Server) snapshotJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	devices := make([]wireDevice, 0, len(s.devices))
	newCount := 0
	for _, d := range s.devices {
		ipStr := d.IP.String()
		e := s.enrich(ipStr)
		devices = append(devices, wireDevice{
			IP: ipStr, MAC: macStr(d.MAC), Vendor: d.Vendor, Hostname: d.Hostname,
			Self: d.IsSelf, Gateway: d.IsGateway, Trusted: s.trusted[ipStr],
			Quarantined: s.quarantined[ipStr],
			OS:          e.OS, JA3: e.JA3, SNI: e.SNI, Anomaly: e.Anomaly, Beacon: e.Beacon,
		})
	}
	alerts := make([]wireAlert, 0, len(s.alerts))
	for _, a := range s.alerts {
		if a.Kind == "new-device" {
			newCount++
		}
		wa := wireOf(a, s.sen.acked[alertKey(a)])
		wa.Clip = s.ref.clipFor[alertKey(a)]
		alerts = append(alerts, wa)
	}
	last := ""
	if !s.lastSweep.IsZero() {
		last = s.lastSweep.Format(time.RFC3339)
	}
	snap := map[string]any{
		"meta": map[string]any{
			"iface": s.info.Iface.Name, "subnet": s.info.IPNet.String(),
			"gateway": gwStr(s.info), "watching": true, "sweeping": s.sweeping,
			"pps": s.pps(), "copilot": s.cop.Name(), "sonarArmed": s.sonarArmed,
			"gate": s.gateLocked(), "recap": s.sen.recapSeq, "rehearsal": s.sen.reh != nil && !s.sen.rehDone,
			"cue": map[string]any{"seq": s.ref.cueSeq, "say": s.ref.cueSay, "ui": s.ref.cueUI},
		},
		"devices": devices,
		"alerts":  alerts,
		"stats":   map[string]any{"new": newCount, "lastSweep": last},
	}
	b, _ := json.Marshal(snap)
	return b
}

// ---- demo ----

func (s *Server) loadDemo() {
	mk := func(ip, mac, vendor, host string, self, gw bool) model.Device {
		m, _ := net.ParseMAC(mac)
		return model.Device{IP: net.ParseIP(ip).To4(), MAC: m, Vendor: vendor, Hostname: host, IsSelf: self, IsGateway: gw, FirstSeen: time.Now(), LastSeen: time.Now()}
	}
	s.mu.Lock()
	s.devices = []model.Device{
		mk("192.168.1.1", "f0:27:2d:aa:bb:cc", "Ubiquiti", "gateway.lan", false, true),
		mk("192.168.1.50", "08:00:27:11:22:33", "VirtualBox", "ronin-rig", true, false),
		mk("192.168.1.20", "b8:27:eb:11:22:33", "Raspberry Pi", "pi-hole.lan", false, false),
		mk("192.168.1.31", "dc:3a:5e:00:11:22", "Amazon", "echo-dot", false, false),
		mk("192.168.1.99", "02:11:22:33:44:55", "(randomized MAC)", "", false, false),
	}
	s.trusted["192.168.1.50"] = true
	s.trusted["192.168.1.20"] = true
	now := time.Now()
	s.alerts = []model.Alert{
		{Time: now.Add(-2 * time.Minute), Severity: model.SevCritical, Kind: "arp-spoof", Source: "192.168.1.1", Message: "gateway MAC changed f0:27:2d:aa:bb:cc → 02:11:22:33:44:55 (possible MITM)"},
		{Time: now.Add(-9 * time.Minute), Severity: model.SevWarn, Kind: "new-device", Source: "192.168.1.99", Message: "new host joined the segment: 192.168.1.99 · randomized MAC (privacy address or evasion)"},
		{Time: now.Add(-22 * time.Minute), Severity: model.SevInfo, Kind: "new-device", Source: "192.168.1.31", Message: "new host joined the segment: 192.168.1.31 (echo-dot) · Amazon"},
	}
	s.lastSweep = now
	full, alerts := s.devices, s.alerts
	s.mu.Unlock()

	// Synthesize the last 30 minutes so the time scrubber has something to replay.
	joined := map[string]time.Time{"192.168.1.31": now.Add(-22 * time.Minute), "192.168.1.99": now.Add(-9 * time.Minute)}
	for m := 30; m >= 0; m-- {
		at := now.Add(-time.Duration(m) * time.Minute)
		var ds []model.Device
		for _, d := range full {
			if j, ok := joined[d.IP.String()]; !ok || !j.After(at) {
				ds = append(ds, d)
			}
		}
		var as []model.Alert
		for _, a := range alerts {
			if !a.Time.After(at) {
				as = append(as, a)
			}
		}
		s.mu.Lock()
		s.devices, s.alerts = ds, as
		s.mu.Unlock()
		s.record(s.snapshotJSON(), at)
	}
	s.mu.Lock()
	s.devices, s.alerts = full, alerts
	s.mu.Unlock()
}

// ---- time scrubber: state history ----

func (s *Server) record(b []byte, at time.Time) {
	if bytes.Contains(b, []byte(`"sweeping":true`)) {
		return // transient scan-in-progress frames are not worth replaying
	}
	var v struct {
		Devices []json.RawMessage `json:"devices"`
		Alerts  []struct {
			Severity string `json:"severity"`
		} `json:"alerts"`
	}
	if json.Unmarshal(b, &v) != nil {
		return
	}
	crit := 0
	for _, a := range v.Alerts {
		if a.Severity == "CRITICAL" {
			crit++
		}
	}
	s.histMu.Lock()
	s.hist = append(s.hist, histEntry{TS: at.UnixMilli(), Hosts: len(v.Devices), Alerts: len(v.Alerts), Crit: crit, snap: b})
	if len(s.hist) > 1500 {
		s.hist = s.hist[len(s.hist)-1500:]
	}
	s.histMu.Unlock()
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	s.histMu.Lock()
	entries := append([]histEntry(nil), s.hist...)
	s.histMu.Unlock()
	writeJSON(w, map[string]any{"entries": entries})
}

func (s *Server) handleHistoryAt(w http.ResponseWriter, r *http.Request) {
	i, err := strconv.Atoi(r.URL.Query().Get("i"))
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if err != nil || i < 0 || i >= len(s.hist) {
		http.Error(w, "no such history entry", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.hist[i].snap)
}

// ---- SONAR: CSI presence sensing ----

func (s *Server) onPresence(node string, score float64) {
	s.mu.Lock()
	s.moves[node]++
	defer s.fireScenes("presence", node, "")
	if s.sen.motion = append(s.sen.motion, time.Now()); len(s.sen.motion) > 4000 {
		s.sen.motion = s.sen.motion[2000:]
	}
	armed := s.sonarArmed
	recent := time.Since(s.lastPresence[node]) < 30*time.Second
	if armed && !recent {
		s.lastPresence[node] = time.Now()
	}
	s.mu.Unlock()
	if !armed || recent {
		return
	}
	s.emitAlert(model.Alert{
		Time: time.Now(), Severity: model.SevWarn, Kind: "presence", Source: node,
		Message: fmt.Sprintf("SONAR armed: movement detected on the %s link (turbulence %.0f/100)", node, score),
	})
}

func (s *Server) handleSonarIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var frames []sonar.Frame
	if json.Unmarshal(body, &frames) != nil {
		var f sonar.Frame
		if json.Unmarshal(body, &f) != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "expected a CSI frame or an array of frames"})
			return
		}
		frames = []sonar.Frame{f}
	}
	now := time.Now()
	for _, f := range frames {
		s.son.Ingest(f, now)
	}
	writeJSON(w, map[string]any{"ok": true, "frames": len(frames)})
}

func (s *Server) handleSonarArm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Armed bool `json:"armed"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	s.sonarArmed = req.Armed
	s.mu.Unlock()
	s.broadcast()
	writeJSON(w, map[string]any{"ok": true, "armed": req.Armed})
}

// sonarDemo feeds three simulated nodes so the SONAR deck works without hardware.
func (s *Server) sonarDemo(ctx context.Context) {
	sims := []*sonar.Sim{sonar.NewSim("node-A", 1), sonar.NewSim("node-B", 2), sonar.NewSim("node-C", 3)}
	sims[0].DistM, sims[1].DistM, sims[2].DistM = 3.4, 4.1, 2.6
	sims[2].BreathBPM = 15 // someone sitting still on the node-C link
	rng, tick := rand.New(rand.NewSource(5)), 0
	t := time.NewTicker(40 * time.Millisecond)
	defer t.Stop()
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		phase := math.Mod(time.Since(start).Seconds(), 30)
		motion := []float64{0, 0, 0}
		if phase > 8 && phase < 14 {
			motion[1] = 1
		}
		if phase > 11 && phase < 13 {
			motion[0] = 0.6
		}
		now := time.Now()
		for i, sim := range sims {
			s.son.Ingest(sim.Frame(motion[i]), now)
		}
		if tick++; tick%25 == 0 {
			s.demoRadios(now, rng)
		}
	}
}

// ---- passive enrichment + globe ----

func (s *Server) enrich(ip string) passive.Enrichment {
	if s.demo {
		return demoEnrichment(ip)
	}
	if s.mon != nil {
		return s.mon.Enrichment(ip)
	}
	return passive.Enrichment{}
}

func (s *Server) pps() uint64 {
	if s.demo {
		return 1200 + uint64(time.Now().Unix()%900)
	}
	if s.mon != nil {
		return s.mon.PPS()
	}
	return 0
}

func (s *Server) handleGlobe(w http.ResponseWriter, r *http.Request) {
	var exts []passive.External
	if s.demo {
		exts = demoExternals()
	} else if s.mon != nil {
		exts = s.mon.Externals()
	}
	type point struct {
		Dst    string  `json:"dst"`
		Port   uint16  `json:"port"`
		Bytes  uint64  `json:"bytes"`
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
		Region string  `json:"region"`
		SNI    string  `json:"sni"`
		Src    string  `json:"src"`
	}
	byDst := map[string]*point{}
	var order []string
	for _, e := range exts {
		p := byDst[e.Dst]
		if p == nil {
			loc := geo.Locate(e.Dst)
			p = &point{Dst: e.Dst, Port: e.DPort, Lat: loc.Lat, Lon: loc.Lon, Region: loc.Region, SNI: e.SNI, Src: e.Src}
			byDst[e.Dst] = p
			order = append(order, e.Dst)
		}
		p.Bytes += e.Bytes
		if p.SNI == "" {
			p.SNI = e.SNI
		}
	}
	pts := make([]*point, 0, len(order))
	for _, d := range order {
		pts = append(pts, byDst[d])
	}
	writeJSON(w, map[string]any{"ok": true, "selfIP": s.info.IP.String(), "points": pts})
}

func demoEnrichment(ip string) passive.Enrichment {
	switch ip {
	case "192.168.1.1":
		return passive.Enrichment{OS: "Network device / Solaris", OSConf: 60}
	case "192.168.1.50":
		return passive.Enrichment{OS: "Linux / Android", OSConf: 66, JA3: "579ccef312d18482fc42e2b822ca2430", SNI: "github.com", Anomaly: 8}
	case "192.168.1.20":
		return passive.Enrichment{OS: "Linux / Android", OSConf: 66, SNI: "cloudflare-dns.com"}
	case "192.168.1.31":
		return passive.Enrichment{OS: "Linux (IoT)", OSConf: 52, JA3: "a0e9f5d64349fb13191bc781f81f42e1", SNI: "device-metrics-us.amazon.com", Anomaly: 16}
	case "192.168.1.99":
		return passive.Enrichment{OS: "macOS / iOS / BSD", OSConf: 58, JA3: "e7d705a3286e19ea42f587b344ee6865", Anomaly: 86, AnomTop: "dsts", Beacon: "185.220.101.44:443 (~60s)"}
	}
	return passive.Enrichment{}
}

func demoExternals() []passive.External {
	return []passive.External{
		{Src: "192.168.1.50", Dst: "140.82.113.25", DPort: 443, Bytes: 184320, SNI: "github.com"},
		{Src: "192.168.1.50", Dst: "142.250.72.14", DPort: 443, Bytes: 240000, SNI: "google.com"},
		{Src: "192.168.1.31", Dst: "52.94.236.248", DPort: 443, Bytes: 33000, SNI: "device-metrics-us.amazon.com"},
		{Src: "192.168.1.99", Dst: "185.220.101.44", DPort: 443, Bytes: 9800},
		{Src: "192.168.1.99", Dst: "45.133.1.90", DPort: 8443, Bytes: 4200},
		{Src: "192.168.1.20", Dst: "1.1.1.1", DPort: 853, Bytes: 12000, SNI: "cloudflare-dns.com"},
	}
}

// ---- helpers ----

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return false
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "bad request"})
		return false
	}
	return true
}

// sameOrigin refuses requests another web page made the browser send: the
// console's actions (firewall, decoys, trust) must only answer the console.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser, or a same-origin request from an old one
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

func auditHandler(fn func() []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "lines": fn()})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func cloneSet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func macStr(m net.HardwareAddr) string {
	if len(m) == 0 {
		return ""
	}
	return m.String()
}

func gwStr(info *netif.Info) string {
	if info.Gateway == nil {
		return "unknown"
	}
	return info.Gateway.String()
}

var _ = log.Println

// ---- SONAR layout + braindance ----

func (s *Server) handleSonarLayout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Layout map[string]braindance.Point `json:"layout"`
	}
	if !decode(w, r, &req) {
		return
	}
	clean := map[string]braindance.Point{}
	for k, v := range req.Layout {
		if len(clean) >= 40 || len(k) == 0 || len(k) > 32 {
			continue
		}
		clean[k] = braindance.Point{X: math.Max(-2, math.Min(2, v.X)), Y: math.Max(-2, math.Min(2, v.Y))}
	}
	s.mu.Lock()
	s.layout = clean
	s.mu.Unlock()
	s.saveRoom()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) layoutCopy() map[string]braindance.Point {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]braindance.Point, len(s.layout))
	for k, v := range s.layout {
		out[k] = v
	}
	return out
}

func (s *Server) handleBD(w http.ResponseWriter, r *http.Request) {
	clips := s.bd.List()
	if clips == nil {
		clips = []braindance.Clip{}
	}
	writeJSON(w, map[string]any{"recordingSince": s.bd.RecordingSince(), "bufferSec": math.Round(s.bd.BufferSeconds()), "stored": s.bd.Dir(), "clips": clips})
}

func (s *Server) handleBDSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string  `json:"name"`
		LastSec float64 `json:"lastSec"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.LastSec <= 0 || req.LastSec > 600 {
		req.LastSec = 60
	}
	now := time.Now()
	c, err := s.bd.Save(req.Name, now.Add(-time.Duration(req.LastSec*float64(time.Second))).UnixMilli(), now.UnixMilli(), s.layoutCopy(), false)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "clip": c})
}

func (s *Server) handleBDRec(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On   bool   `json:"on"`
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.On {
		s.bd.StartRec(time.Now())
		writeJSON(w, map[string]any{"ok": true, "recording": true})
		return
	}
	c, err := s.bd.StopRec(req.Name, time.Now(), s.layoutCopy())
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "clip": c})
}

func (s *Server) handleBDClip(w http.ResponseWriter, r *http.Request) {
	c, ok := s.bd.Load(r.URL.Query().Get("id"))
	if !ok {
		http.Error(w, "no such clip", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

func (s *Server) handleBDDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	writeJSON(w, map[string]any{"ok": s.bd.Delete(req.ID)})
}

// ---- process lineage ----

func (s *Server) lineage() []audit.ProcConn {
	if s.demo {
		return []audit.ProcConn{
			{PID: 4121, Comm: "firefox", Exe: "/usr/lib/firefox/firefox", Remote: "140.82.113.25:443", Public: true},
			{PID: 4121, Comm: "firefox", Exe: "/usr/lib/firefox/firefox", Remote: "142.250.72.14:443", Public: true},
			{PID: 912, Comm: "syncthing", Exe: "/usr/bin/syncthing", Remote: "192.168.1.20:22000"},
		}
	}
	return audit.Lineage()
}

func (s *Server) lineageLines() []string {
	if !s.demo {
		return audit.LineageLines()
	}
	lines := []string{"◆ PROCESS LINEAGE · sample data · [T1071 — which program is talking]"}
	for _, c := range s.lineage() {
		flag := ""
		if c.Public {
			flag = "  ⚠ PUBLIC"
		}
		lines = append(lines, fmt.Sprintf("%-16s pid %-7d → %s%s", c.Comm, c.PID, c.Remote, flag))
	}
	return lines
}

func (s *Server) handleLineage(w http.ResponseWriter, r *http.Request) {
	conns := s.lineage()
	if conns == nil {
		conns = []audit.ProcConn{}
	}
	writeJSON(w, map[string]any{"conns": conns})
}

// ---- presence gate ----

// gateLocked reports the gate state for the console. Caller holds s.mu.
func (s *Server) gateLocked() map[string]any {
	present := false
	for _, d := range s.devices {
		if s.gateMAC != "" && macStr(d.MAC) == s.gateMAC {
			present = true
		}
	}
	return map[string]any{"enabled": s.gateMAC != "", "key": s.gateName, "present": present, "open": s.gateMAC == "" || present}
}

// gateRefusal returns a message when the key device is required but absent.
func (s *Server) gateRefusal() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.gateLocked(); !g["open"].(bool) {
		return "locked: presence key " + s.gateName + " is not on the network"
	}
	return ""
}

func (s *Server) handleGate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"` // empty clears the gate
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	if req.IP == "" {
		if g := s.gateLocked(); !g["open"].(bool) {
			s.mu.Unlock()
			writeJSON(w, map[string]any{"ok": false, "error": "the key device must be present to remove the gate"})
			return
		}
		s.gateMAC, s.gateName = "", ""
	} else {
		found := false
		for _, d := range s.devices {
			if d.IP.String() == req.IP && len(d.MAC) > 0 {
				s.gateMAC, found = macStr(d.MAC), true
				s.gateName = d.Hostname
				if s.gateName == "" {
					s.gateName = req.IP
				}
			}
		}
		if !found {
			s.mu.Unlock()
			writeJSON(w, map[string]any{"ok": false, "error": "that host is not on the segment"})
			return
		}
	}
	s.mu.Unlock()
	s.broadcast()
	writeJSON(w, map[string]any{"ok": true})
}
