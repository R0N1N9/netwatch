package server

// Rehearsal, "while you were out", the operator fatigue meter, alert
// acknowledgement and the scanner overlay's data.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/r0n1n9/netwatch/internal/cortex"
	"github.com/r0n1n9/netwatch/internal/fatigue"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/oracle"
	"github.com/r0n1n9/netwatch/internal/recap"
	"github.com/r0n1n9/netwatch/internal/rehearsal"
)

// senses is guarded by Server.mu.
type senses struct {
	acked map[string]bool

	// incident rehearsal
	reh       *rehearsal.Session
	rehDone   bool
	rehScores []map[string]any

	// operator watch (off until switched on; nothing is stored)
	opOn      bool
	opStart   time.Time
	opLast    time.Time
	opLevel   string
	opReading *fatigue.Reading

	// while you were out
	away      recap.Tracker
	motion    []time.Time // movement onsets sensed by SONAR
	lastRecap *recap.Recap
	recapSeq  int
}

func (s *Server) initSenses() {
	s.sen = senses{acked: map[string]bool{}, away: recap.Tracker{AwayAfter: 10 * time.Minute}}
}

func (s *Server) senseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/rehearsal", s.handleRehearsal)
	mux.HandleFunc("/api/ack", s.handleAck)
	mux.HandleFunc("/api/operator", s.handleOperator)
	mux.HandleFunc("/api/recap", s.handleRecap)
	mux.HandleFunc("/api/scanner", s.handleScanner)
}

func (s *Server) sensesLoop(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.rehearsalTick(now)
			s.presenceTick(now)
			if n++; n%20 == 0 {
				s.operatorTick(now)
			}
		}
	}
}

// ---- acknowledgement ----

func (s *Server) isAcked(a model.Alert) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sen.acked[alertKey(a)]
}

func (s *Server) ack(a model.Alert) {
	s.mu.Lock()
	s.sen.acked[alertKey(a)] = true
	s.mu.Unlock()
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TS     int64  `json:"ts"`
		Source string `json:"source"`
		Kind   string `json:"kind"`
		All    bool   `json:"all"`
		Newest bool   `json:"newest"`
	}
	if !decode(w, r, &req) {
		return
	}
	n := 0
	s.mu.Lock()
	for _, a := range s.alerts {
		k := alertKey(a)
		if s.sen.acked[k] {
			continue
		}
		if req.All || req.Newest || (a.Time.UnixMilli() == req.TS && a.Source == req.Source && a.Kind == req.Kind) {
			s.sen.acked[k] = true
			n++
			if req.Newest {
				break
			}
		}
	}
	s.mu.Unlock()
	if n > 0 {
		s.broadcast()
	}
	writeJSON(w, map[string]any{"ok": true, "acknowledged": n})
}

// ---- incident rehearsal ----

var rehearsalMAC = net.HardwareAddr{0x02, 0xde, 0xad, 0x00, 0xbe, 0xef}

// rehearsalHostLocked returns the synthetic intruder's address: .250 of the subnet.
func (s *Server) rehearsalIntruder() string {
	ip := append(net.IP(nil), s.info.IPNet.IP.To4()...)
	if ip == nil {
		return "192.168.250.250"
	}
	ip[3] = 250
	return ip.String()
}

// withRehearsalHost keeps the drill's synthetic host in the device list across sweeps. Caller holds s.mu.
func (s *Server) withRehearsalHost(devs []model.Device) []model.Device {
	if s.sen.reh == nil || s.sen.rehDone || s.sen.reh.State().Scenario.Target != "intruder" {
		return devs
	}
	host := s.sen.reh.Host()
	for _, d := range devs {
		if d.IP.String() == host {
			return devs
		}
	}
	now := time.Now()
	return append(devs, model.Device{IP: net.ParseIP(host).To4(), MAC: rehearsalMAC, Vendor: "(randomized MAC)", Hostname: "REHEARSAL-GHOST", FirstSeen: now, LastSeen: now})
}

func (s *Server) rehearsalState() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sen.reh == nil {
		return nil
	}
	return s.sen.reh.State()
}

func (s *Server) handleRehearsal(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.mu.Lock()
		scores := append([]map[string]any{}, s.sen.rehScores...)
		s.mu.Unlock()
		writeJSON(w, map[string]any{"scenarios": rehearsal.Scenarios(), "state": s.rehearsalState(), "scores": scores})
		return
	}
	var req struct {
		Start string `json:"start"`
		Stop  bool   `json:"stop"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Stop {
		s.rehearsalFinish(true)
		writeJSON(w, map[string]any{"ok": true, "state": s.rehearsalState()})
		return
	}
	sc, ok := rehearsal.Find(req.Start)
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "no such scenario"})
		return
	}
	host := s.rehearsalIntruder()
	if sc.Target == "gateway" {
		if host = gwStr(s.info); net.ParseIP(host) == nil {
			s.mu.Lock()
			for _, d := range s.devices {
				if d.IsGateway {
					host = d.IP.String()
				}
			}
			s.mu.Unlock()
		}
		if net.ParseIP(host) == nil {
			writeJSON(w, map[string]any{"ok": false, "error": "this scenario needs a known gateway"})
			return
		}
	}
	s.mu.Lock()
	if s.sen.reh != nil && !s.sen.rehDone {
		s.mu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "error": "a rehearsal is already running"})
		return
	}
	s.sen.reh, s.sen.rehDone = rehearsal.Start(sc, host, time.Now()), false
	s.devices = s.withRehearsalHost(s.devices)
	s.mu.Unlock()
	s.logAction("rehearsal", "started the rehearsal \""+sc.Name+"\" — every alert it raises is synthetic")
	s.broadcast()
	writeJSON(w, map[string]any{"ok": true, "state": s.rehearsalState()})
}

func sevOf(name string) model.Severity {
	switch name {
	case "CRITICAL":
		return model.SevCritical
	case "WARN":
		return model.SevWarn
	}
	return model.SevInfo
}

func (s *Server) rehearsalTick(now time.Time) {
	s.mu.Lock()
	sess := s.sen.reh
	if sess == nil || s.sen.rehDone {
		s.mu.Unlock()
		return
	}
	due := sess.Due(now)
	ended := sess.Tick(now)
	host := sess.Host()
	s.mu.Unlock()
	for _, st := range due {
		s.emitAlert(model.Alert{Time: now, Severity: sevOf(st.Severity), Kind: st.Kind, Source: host, Message: rehearsal.Label + st.Message})
	}
	if ended {
		s.rehearsalFinish(false)
	}
}

// rehearsalFinish closes the drill, removes its synthetic host and records the score.
func (s *Server) rehearsalFinish(stopped bool) {
	s.mu.Lock()
	sess := s.sen.reh
	if sess == nil || s.sen.rehDone {
		s.mu.Unlock()
		return
	}
	sess.Stop()
	s.sen.rehDone = true
	st := sess.State()
	if st.Scenario.Target == "intruder" {
		keep := s.devices[:0]
		for _, d := range s.devices {
			if d.IP.String() != st.Host {
				keep = append(keep, d)
			}
		}
		s.devices = keep
		delete(s.quarantined, st.Host)
		delete(s.trusted, st.Host)
	}
	s.sen.rehScores = append([]map[string]any{{"at": time.Now().UnixMilli(), "scenario": st.Scenario.Name, "score": st.Score, "verdict": st.Verdict, "stopped": stopped}}, s.sen.rehScores...)
	if len(s.sen.rehScores) > 20 {
		s.sen.rehScores = s.sen.rehScores[:20]
	}
	s.mu.Unlock()
	how := "finished"
	if stopped {
		how = "stopped early"
	}
	s.logAction("rehearsal", fmt.Sprintf("rehearsal \"%s\" %s: %d/100 — %s", st.Scenario.Name, how, st.Score, st.Verdict))
	s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevInfo, Kind: "rehearsal", Source: "rehearsal",
		Message: fmt.Sprintf("%srehearsal %s: %d/100 — %s. Nothing real was touched.", rehearsal.Label, how, st.Score, st.Verdict)})
}

// rehearsalAct credits (or penalises) something the operator did. host "" matches any drill.
func (s *Server) rehearsalAct(host, action string) (matched bool, penalty string) {
	s.mu.Lock()
	sess := s.sen.reh
	if sess == nil || s.sen.rehDone || (host != "" && host != sess.Host()) {
		s.mu.Unlock()
		return false, ""
	}
	matched, penalty = sess.Act(action, time.Now())
	s.mu.Unlock()
	return matched, penalty
}

// rehearsalBlackwall answers a quarantine or release aimed at the drill's host
// without touching the firewall. It reports whether it handled the request.
func (s *Server) rehearsalBlackwall(w http.ResponseWriter, ip, action string) bool {
	s.mu.Lock()
	sess := s.sen.reh
	if sess == nil || s.sen.rehDone || ip != sess.Host() {
		s.mu.Unlock()
		return false
	}
	target := sess.State().Scenario.Target
	s.mu.Unlock()
	if action != "quarantine" {
		s.mu.Lock()
		delete(s.quarantined, ip)
		s.mu.Unlock()
		s.broadcast()
		writeJSON(w, map[string]any{"ok": true, "note": "rehearsal: released (simulated)"})
		return true
	}
	_, penalty := s.rehearsalAct(ip, "quarantine")
	if target != "intruder" {
		s.logAction("rehearsal", "rehearsal: tried to quarantine "+ip+" — refused, nothing was changed")
		writeJSON(w, map[string]any{"ok": false, "error": rehearsal.Label + penalty + ". In a real incident that would have cut everyone off. Nothing was changed."})
		return true
	}
	s.mu.Lock()
	s.quarantined[ip] = true
	s.mu.Unlock()
	s.logAction("rehearsal", "rehearsal: quarantined the synthetic host "+ip+" (the firewall was not touched)")
	s.broadcast()
	writeJSON(w, map[string]any{"ok": true, "note": "rehearsal: quarantined (simulated — the firewall was not touched)"})
	return true
}

// edges is every observed conversation, plus the drill's synthetic ones while a rehearsal runs.
func (s *Server) edges() []oracle.Edge {
	out := s.observedEdges()
	s.mu.Lock()
	sess := s.sen.reh
	live := sess != nil && !s.sen.rehDone && sess.State().Scenario.Target == "intruder"
	s.mu.Unlock()
	if live {
		h, self := sess.Host(), s.info.IP.String()
		out = append(out, oracle.Edge{Src: h, Dst: gwStr(s.info), Port: 80, Bytes: 2100}, oracle.Edge{Src: h, Dst: self, Port: 2323, Bytes: 96},
			oracle.Edge{Src: h, Dst: "203.0.113.44", Port: 443, Bytes: 8800, External: true})
	}
	return out
}

// ---- operator watch and fatigue ----

func (s *Server) operatorTick(now time.Time) {
	s.mu.Lock()
	if !s.sen.opOn {
		s.sen.opReading = nil
		s.mu.Unlock()
		return
	}
	in := fatigue.Input{Hour: now.Hour(), SessionMin: s.sen.opLast.Sub(s.sen.opStart).Minutes()}
	for _, a := range s.alerts {
		if a.Severity == model.SevCritical && !s.sen.acked[alertKey(a)] && now.Sub(a.Time) < 6*time.Hour {
			in.Unopened++
			if age := now.Sub(a.Time).Minutes(); age > in.OldestMin {
				in.OldestMin = age
			}
		}
	}
	s.mu.Unlock()
	in.HoldDrift, in.GapDrift, in.SpreadRatio, in.HaveRhythm = s.mod.pulse.Drift()
	r := fatigue.Assess(in)
	s.mu.Lock()
	was := s.sen.opLevel
	s.sen.opLevel, s.sen.opReading = r.Level, &r
	s.mu.Unlock()
	if r.Level == "spent" && was != "spent" {
		s.emitAlert(model.Alert{Time: now, Severity: model.SevInfo, Kind: "operator", Source: "operator",
			Message: fmt.Sprintf("operator sharpness is down to %d/100 — %s", r.Sharpness, r.Advice)})
	}
}

func (s *Server) handleOperator(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if r.Method == http.MethodPost {
		var req struct {
			On   *bool `json:"on"`
			Ping bool  `json:"ping"`
		}
		if !decode(w, r, &req) {
			return
		}
		s.mu.Lock()
		if req.On != nil {
			s.sen.opOn = *req.On
			s.sen.opStart, s.sen.opLast, s.sen.opLevel, s.sen.opReading = now, now, "", nil
		}
		if req.Ping && s.sen.opOn {
			if now.Sub(s.sen.opLast) > 15*time.Minute { // a real break: a new session starts
				s.sen.opStart = now
			}
			s.sen.opLast = now
		}
		s.mu.Unlock()
		if req.On != nil {
			s.operatorTick(now)
		}
	}
	s.mu.Lock()
	out := map[string]any{"ok": true, "on": s.sen.opOn, "reading": s.sen.opReading}
	if s.sen.opOn {
		out["sessionMin"] = s.sen.opLast.Sub(s.sen.opStart).Minutes()
	}
	s.mu.Unlock()
	writeJSON(w, out)
}

// ---- while you were out ----

// presenceTick decides whether anyone is around: SONAR sensing movement or
// breathing, or (with operator watch on) recent input at the console.
func (s *Server) presenceTick(now time.Time) {
	nodes := s.son.Snapshot(now)
	haveSonar, present := false, false
	for _, n := range nodes {
		if n.State != "offline" && n.State != "calibrating" {
			haveSonar = true
		}
		if n.State == "motion" || n.Breath > 0 {
			present = true
		}
	}
	s.mu.Lock()
	if s.sen.opOn && now.Sub(s.sen.opLast) < 90*time.Second {
		present = true
	}
	if !haveSonar && !s.sen.opOn {
		s.mu.Unlock()
		return // nothing to judge presence by
	}
	span := s.sen.away.Feed(present, now)
	s.mu.Unlock()
	if span != nil {
		s.makeRecap(*span)
	}
}

// hostsAt lists the hosts in the newest history snapshot at or before t.
func (s *Server) hostsAt(t time.Time) []string {
	s.histMu.Lock()
	var snap []byte
	if len(s.hist) > 0 {
		snap = s.hist[0].snap // history starts after t: the oldest snapshot is the best there is
	}
	for _, h := range s.hist {
		if h.TS <= t.UnixMilli() {
			snap = h.snap
		}
	}
	s.histMu.Unlock()
	var v struct {
		Devices []struct {
			IP       string `json:"ip"`
			Hostname string `json:"hostname"`
		} `json:"devices"`
	}
	if snap == nil || json.Unmarshal(snap, &v) != nil {
		return nil
	}
	out := make([]string, 0, len(v.Devices))
	for _, d := range v.Devices {
		out = append(out, hostLabel(d.Hostname, d.IP))
	}
	return out
}

func hostLabel(name, ip string) string {
	if name != "" {
		return name + " (" + ip + ")"
	}
	return ip
}

func (s *Server) makeRecap(span recap.Span) recap.Recap {
	before := s.hostsAt(span.From)
	s.mu.Lock()
	var events []recap.Event
	for _, a := range s.alerts {
		events = append(events, recap.Event{T: a.Time.UnixMilli(), Severity: a.Severity.String(), Kind: a.Kind, Text: a.Message})
	}
	after := make([]string, 0, len(s.devices))
	for _, d := range s.devices {
		after = append(after, hostLabel(d.Hostname, d.IP.String()))
	}
	moves := 0
	for _, m := range s.sen.motion {
		if !m.Before(span.From) && !m.After(span.To) {
			moves++
		}
	}
	s.mu.Unlock()
	if before == nil {
		before = after // no history at all: report no change rather than everything as new
	}
	r := recap.Build(span, events, before, after, moves)
	s.mu.Lock()
	s.sen.lastRecap = &r
	s.sen.recapSeq++
	s.mu.Unlock()
	s.broadcast()
	return r
}

func (s *Server) handleRecap(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Mins float64 `json:"mins"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.Mins <= 0 || req.Mins > 24*60 {
			req.Mins = 30
		}
		now := time.Now()
		rc := s.makeRecap(recap.Span{From: now.Add(-time.Duration(req.Mins * float64(time.Minute))), To: now})
		writeJSON(w, map[string]any{"ok": true, "recap": rc})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, map[string]any{"recap": s.sen.lastRecap, "seq": s.sen.recapSeq, "away": s.sen.away.Away(), "awayAfterMin": s.sen.away.AwayAfter.Minutes()})
}

// ---- scanner overlay ----

func (s *Server) handleScanner(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	devs := append([]model.Device{}, s.devices...)
	alerts := append([]model.Alert{}, s.alerts...)
	trusted, quarantined := cloneSet(s.trusted), cloneSet(s.quarantined)
	trips := s.mod.demoTrips
	s.mu.Unlock()
	if !s.demo {
		trips = s.mod.dec.Trips()
	}
	edges, self := s.edges(), s.info.IP.String()
	devCount := map[string]int{}
	for _, d := range s.mod.ward.Deviations() {
		devCount[d.Host]++
	}
	hosts := []map[string]any{}
	for _, d := range devs {
		ip := d.IP.String()
		e := s.enrich(ip)
		role := "unverified"
		switch {
		case d.IsSelf:
			role = "self"
		case d.IsGateway:
			role = "gateway"
		case trusted[ip]:
			role = "trusted"
		}
		h := map[string]any{"ip": ip, "name": d.Hostname, "vendor": d.Vendor, "role": role, "quarantined": quarantined[ip],
			"os": e.OS, "anomaly": e.Anomaly, "beacon": e.Beacon, "deviations": devCount[ip]}
		n, worst := 0, ""
		for _, a := range alerts {
			if a.Source != ip {
				continue
			}
			if n == 0 { // newest alert from this host: its case
				c := cortex.Investigate(s.evidence(a))
				h["verdict"], h["confidence"], h["proposal"], h["alert"] = c.Verdict, c.Confidence, c.Proposal.Action, wireOf(a, false)
			}
			n++
			if a.Severity == model.SevCritical || (a.Severity == model.SevWarn && worst != "CRITICAL") || worst == "" {
				worst = a.Severity.String()
			}
		}
		h["alerts"], h["worst"] = n, worst
		tn := 0
		for _, t := range trips {
			if t.Source == ip {
				tn++
			}
		}
		h["trips"] = tn
		re := oracle.ReachOf(ip, self, edges)
		h["inside"], h["outside"], h["toThis"] = len(re.Internal)+len(re.Inbound), len(re.External), re.ToThis
		var names []string
		for _, x := range re.External {
			if x.Name != "" && len(names) < 3 && !strings.Contains(strings.Join(names, " "), x.Name) {
				names = append(names, x.Name)
			}
		}
		h["outsideNames"] = names
		hosts = append(hosts, h)
	}
	writeJSON(w, map[string]any{"hosts": hosts})
}
