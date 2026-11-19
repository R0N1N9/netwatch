package server

// The BRAINDANCE deck and the phone's BD tab: room clips with the floor plan
// and the movement estimate worked out for each instant, and "phone views" —
// a short memory of where a paired phone stood and what it was facing.

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/r0n1n9/netwatch/internal/braindance"
	"github.com/r0n1n9/netwatch/internal/optic"
	"github.com/r0n1n9/netwatch/internal/sonar"
)

// wallSegs is the floor plan as drawn in SONAR, for keeping with a clip.
func (s *Server) wallSegs() []braindance.Segment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]braindance.Segment, 0, len(s.walls))
	for _, w := range s.walls {
		out = append(out, braindance.Segment{A: braindance.Point(w.A), B: braindance.Point(w.B)})
	}
	return out
}

// danceList is the clip library without the samples.
func (s *Server) danceList() map[string]any {
	clips := s.bd.List()
	if clips == nil {
		clips = []braindance.Clip{}
	}
	return map[string]any{"bufferSec": math.Round(s.bd.BufferSeconds()), "clips": clips, "demo": s.demo}
}

// danceScene is one clip ready to draw: its samples, the room as it was drawn
// when the clip was cut (or as it is now, for clips saved before the plan was
// kept), where the movement was at each sample (null when nothing moved) and
// the rough head count at each sample.
func (s *Server) danceScene(id string) (map[string]any, bool) {
	c, ok := s.bd.Load(id)
	if !ok {
		return nil, false
	}
	lay := map[string]sonar.Pt{}
	for k, v := range c.Layout {
		lay[k] = sonar.Pt(v)
	}
	if len(lay) == 0 {
		lay = s.sonarLayout()
	}
	walls, planThen := c.Walls, len(c.Walls) > 0
	if !planThen {
		walls = s.wallSegs()
	}
	est := make([]*[2]float64, len(c.Samples))
	people := make([]int, len(c.Samples))
	for i, smp := range c.Samples {
		nodes := make([]sonar.NodeState, 0, len(smp.Nodes))
		for _, n := range smp.Nodes {
			nodes = append(nodes, sonar.NodeState{Node: n.Node, State: n.State, Score: n.Score, Breath: n.Breath, DistM: n.DistM})
		}
		people[i] = sonar.People(nodes, lay)
		if p, ok := sonar.Estimate(nodes, lay); ok {
			est[i] = &[2]float64{math.Round(p.X*1000) / 1000, math.Round(p.Y*1000) / 1000}
		}
	}
	return map[string]any{"id": c.ID, "name": c.Name, "start": c.Start, "end": c.End, "peak": c.Peak, "auto": c.Auto,
		"layout": lay, "walls": walls, "planThen": planThen, "samples": c.Samples, "est": est, "people": people}, true
}

func (s *Server) handleDanceScene(w http.ResponseWriter, r *http.Request) {
	v, ok := s.danceScene(r.URL.Query().Get("id"))
	if !ok {
		http.Error(w, "no such clip", http.StatusNotFound)
		return
	}
	writeJSON(w, v)
}

// ---- phone views ----

// things is everything on the room map a phone could be facing.
func (s *Server) things() []optic.Thing {
	v := s.sonarView()
	lay, _ := v["layout"].(map[string]sonar.Pt)
	out := []optic.Thing{}
	if ap, ok := lay[sonar.APKey]; ok {
		out = append(out, optic.Thing{Kind: "ap", Label: "ACCESS POINT", X: ap.X, Y: ap.Y})
	}
	nodes, _ := v["nodes"].([]sonar.NodeState)
	for _, n := range nodes {
		if p, ok := lay[n.Node]; ok {
			out = append(out, optic.Thing{Kind: "node", Label: strings.ToUpper(n.Node), X: p.X, Y: p.Y})
		}
	}
	hosts, _ := v["hosts"].([]sonarHost)
	for _, h := range hosts {
		name := h.Name
		if name == "" {
			name = h.IP
		}
		out = append(out, optic.Thing{Kind: "host", Label: name, X: h.X, Y: h.Y})
	}
	if est, ok := v["est"].(map[string]any); ok && est["ok"] == true {
		x, _ := est["x"].(float64)
		y, _ := est["y"].(float64)
		out = append(out, optic.Thing{Kind: "presence", Label: "PRESENCE", X: x, Y: y})
	}
	return out
}

// phoneName is what the console calls a phone: its host name if the watch has
// seen it on the network, otherwise its address.
func (s *Server) phoneName(ip string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.IP.String() == ip && d.Hostname != "" {
			return d.Hostname
		}
	}
	return ip
}

// handlePhoneView takes one frame of a phone's view. It is the only thing a
// phone can send, and only while SHARE MY VIEW is on in its BD tab.
func (s *Server) handlePhoneView(w http.ResponseWriter, r *http.Request) {
	if ok, code := s.paired(r); !ok {
		http.Error(w, "pairing code required", code)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 600))
	var f optic.Frame
	if err != nil || json.Unmarshal(body, &f) != nil {
		http.Error(w, "bad frame", http.StatusBadRequest)
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if f.Target != "" { // only a device the console already knows may be named
		known := false
		for _, t := range s.sonarHosts() {
			known = known || t.MAC == f.Target
		}
		if !known {
			f.Target = ""
		}
	}
	kept := s.opt.Add(ip, s.phoneName(ip), f, s.things(), time.Now())
	writeJSON(w, map[string]any{"ok": true, "kept": kept})
}

func (s *Server) sonarHosts() []sonarHost {
	h, _ := s.sonarView()["hosts"].([]sonarHost)
	return h
}

// handleOptic lists the phone views for the console, and forgets them on request.
func (s *Server) handleOptic(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Forget string `json:"forget"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.Forget != "" {
			s.opt.Forget(req.Forget)
			s.logAction("braindance", "forgot the phone view of "+req.Forget)
		}
	}
	writeJSON(w, map[string]any{"ok": true, "demo": s.demo, "phones": s.opt.Phones(time.Now())})
}

// handleOpticTrail is one phone's frames, with the room as it is now to draw them in.
func (s *Server) handleOpticTrail(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	v := s.sonarView()
	writeJSON(w, map[string]any{"phone": q.Get("phone"), "frames": s.opt.Trail(q.Get("phone"), since),
		"layout": v["layout"], "walls": v["walls"], "nodes": v["nodes"], "hosts": v["hosts"], "est": v["est"], "people": v["people"], "fov": optic.FOV})
}

// demoOptic walks a sample phone round the demo room so the BRAINDANCE deck
// has a view to play. It lives in memory like every phone view.
func (s *Server) demoOptic(ctx context.Context) {
	const phone, name = "192.168.1.60", "sample phone"
	pose := func(t float64) optic.Frame {
		a := t / 7
		f := optic.Frame{X: 0.55 * math.Cos(a), Y: 0.35 + 0.45*math.Sin(a), Mode: "xray", Pitch: 4 * math.Sin(t/3)}
		f.Heading = math.Mod(a*180/math.Pi+180+40*math.Sin(t/2.2)+720, 360) // walking a loop, looking around
		if math.Mod(t, 40) > 22 {
			f.Mode, f.Target = "find", "dc:3a:5e:00:11:22" // looking for the echo-dot
		}
		return f
	}
	start := time.Now()
	things := s.things()
	for i := 240; i > 0; i-- { // the two minutes before the console started
		at := start.Add(-time.Duration(i) * 500 * time.Millisecond)
		s.opt.Add(phone, name, pose(float64(at.UnixMilli())/1000), things, at)
	}
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.opt.Add(phone, name, pose(float64(now.UnixMilli())/1000), s.things(), now)
			}
		}
	}()
}
