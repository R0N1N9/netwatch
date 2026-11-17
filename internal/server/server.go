// Package server hosts the NetWatch GUI: it serves the embedded web console and
// streams live network state to it over Server-Sent Events, while exposing
// action endpoints (sweep, trust, BLACKWALL quarantine, offensive scan, copilot).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/r0n1n9/netwatch/internal/blackwall"
	"github.com/r0n1n9/netwatch/internal/copilot"
	"github.com/r0n1n9/netwatch/internal/ice"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/netif"
	"github.com/r0n1n9/netwatch/internal/offensive"
	"github.com/r0n1n9/netwatch/internal/sweep"
	"github.com/r0n1n9/netwatch/internal/webui"
)

// Server is the NetWatch backend.
type Server struct {
	info      *netif.Info
	engine    *ice.Engine
	wall      *blackwall.Wall
	cop       copilot.Copilot
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
}

// New builds a server for an interface.
func New(info *netif.Info, autoEvery time.Duration, demo bool) *Server {
	return &Server{
		info:        info,
		engine:      ice.New(),
		wall:        blackwall.New(),
		cop:         copilot.Default(),
		autoEvery:   autoEvery,
		demo:        demo,
		trusted:     map[string]bool{},
		quarantined: map[string]bool{},
		clients:     map[chan []byte]struct{}{},
	}
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
	return mux
}

// Run starts the background watch loop and the HTTP server.
func (s *Server) Run(ctx context.Context, addr string) error {
	if s.demo {
		s.loadDemo()
	} else {
		go s.watchLoop(ctx)
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

	s.mu.Lock()
	s.sweeping = false
	s.lastSweep = time.Now()
	if err == nil {
		for i := range devices {
			devices[i].Trusted = s.trusted[devices[i].IP.String()]
		}
		s.devices = devices
		newAlerts := s.engine.Observe(devices, trusted)
		for _, a := range newAlerts {
			s.alerts = append([]model.Alert{a}, s.alerts...)
		}
		if len(s.alerts) > 200 {
			s.alerts = s.alerts[:200]
		}
	}
	s.mu.Unlock()
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
	s.mu.Lock()
	s.trusted[req.IP] = req.Trusted
	for i := range s.devices {
		if s.devices[i].IP.String() == req.IP {
			s.devices[i].Trusted = req.Trusted
		}
	}
	s.mu.Unlock()
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
	var err error
	switch req.Action {
	case "quarantine":
		err = s.wall.Quarantine(req.IP)
		if err == nil {
			s.mu.Lock()
			s.quarantined[req.IP] = true
			s.alerts = append([]model.Alert{{
				Time: time.Now(), Severity: model.SevWarn, Kind: "blackwall",
				Source: req.IP, Message: "BLACKWALL engaged — " + req.IP + " quarantined at the firewall",
			}}, s.alerts...)
			s.mu.Unlock()
		}
	case "release":
		err = s.wall.Release(req.IP)
		if err == nil {
			s.mu.Lock()
			delete(s.quarantined, req.IP)
			s.mu.Unlock()
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
		Iface:       s.info.Iface.Name,
		Subnet:      s.info.IPNet.String(),
	}
	s.mu.Unlock()
	answer := s.cop.Ask(r.Context(), req.Query, snap)
	writeJSON(w, map[string]any{"answer": answer})
}

// ---- snapshot ----

type wireDevice struct {
	IP          string `json:"ip"`
	MAC         string `json:"mac"`
	Vendor      string `json:"vendor"`
	Hostname    string `json:"hostname"`
	Self        bool   `json:"self"`
	Gateway     bool   `json:"gateway"`
	Trusted     bool   `json:"trusted"`
	Quarantined bool   `json:"quarantined"`
}

type wireAlert struct {
	TS       int64  `json:"ts"`
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

func (s *Server) snapshotJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	devices := make([]wireDevice, 0, len(s.devices))
	newCount := 0
	for _, d := range s.devices {
		ipStr := d.IP.String()
		devices = append(devices, wireDevice{
			IP: ipStr, MAC: macStr(d.MAC), Vendor: d.Vendor, Hostname: d.Hostname,
			Self: d.IsSelf, Gateway: d.IsGateway, Trusted: s.trusted[ipStr],
			Quarantined: s.quarantined[ipStr],
		})
	}
	alerts := make([]wireAlert, 0, len(s.alerts))
	for _, a := range s.alerts {
		if a.Kind == "new-device" {
			newCount++
		}
		alerts = append(alerts, wireAlert{
			TS: a.Time.UnixMilli(), Severity: a.Severity.String(),
			Kind: a.Kind, Source: a.Source, Message: a.Message,
		})
	}
	last := ""
	if !s.lastSweep.IsZero() {
		last = s.lastSweep.Format(time.RFC3339)
	}
	snap := map[string]any{
		"meta": map[string]any{
			"iface": s.info.Iface.Name, "subnet": s.info.IPNet.String(),
			"gateway": gwStr(s.info), "watching": true, "sweeping": s.sweeping,
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
	s.alerts = []model.Alert{
		{Time: time.Now(), Severity: model.SevCritical, Kind: "arp-spoof", Source: "192.168.1.1", Message: "gateway MAC changed f0:27:2d:aa:bb:cc → 02:11:22:33:44:55 (possible MITM)"},
		{Time: time.Now().Add(-time.Minute), Severity: model.SevWarn, Kind: "new-device", Source: "192.168.1.99", Message: "new host joined the segment: 192.168.1.99 · randomized MAC (privacy address or evasion)"},
	}
	s.lastSweep = time.Now()
	s.mu.Unlock()
}

// ---- helpers ----

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "bad request"})
		return false
	}
	return true
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
