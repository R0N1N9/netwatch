package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r0n1n9/netwatch/internal/braindance"
	"github.com/r0n1n9/netwatch/internal/sonar"
)

// danceServer has a room, a wall and a ten-second clip with movement in its second half.
func danceServer(t *testing.T) (*Server, string) {
	s := testServer(t)
	s.layout = map[string]braindance.Point{"@ap": {X: 0, Y: 0}, "node-A": {X: 1, Y: 0}}
	s.walls = []sonar.Wire{{ID: "w", A: sonar.Pt{X: -1, Y: -1}, B: sonar.Pt{X: 1, Y: -1}}}
	now := time.Now()
	for i := 0; i < 50; i++ {
		n := braindance.NodeSample{Node: "node-A", State: "clear", Score: 2}
		if i >= 25 {
			n.State, n.Score = "motion", 60
		}
		s.bd.Add(braindance.Sample{T: now.Add(time.Duration(i-50) * 200 * time.Millisecond).UnixMilli(), Nodes: []braindance.NodeSample{n}})
	}
	c, err := s.bd.Save("walk", now.Add(-time.Minute).UnixMilli(), now.UnixMilli(), s.layoutCopy(), false, s.wallSegs()...)
	if err != nil {
		t.Fatal(err)
	}
	return s, c.ID
}

func TestDanceSceneHasThePlanAndWhereTheMovementWas(t *testing.T) {
	s, id := danceServer(t)
	s.walls = nil // the plan is redrawn later: the clip keeps the one it was cut with
	v, ok := s.danceScene(id)
	if !ok {
		t.Fatal("clip not found")
	}
	if walls := v["walls"].([]braindance.Segment); len(walls) != 1 || walls[0].B.X != 1 || v["planThen"] != true {
		t.Errorf("the clip should carry its own floor plan: %+v", v["walls"])
	}
	est := v["est"].([]*[2]float64)
	if len(est) != 50 || est[0] != nil || est[49] == nil {
		t.Fatalf("est should be null while nothing moved and set once it did: first %v last %v (n=%d)", est[0], est[49], len(est))
	}
	if p := est[49]; p[0] < 0.2 || p[0] > 0.8 || p[1] != 0 {
		t.Errorf("movement on the AP–node link should be placed between them, got %v", *p)
	}
	if _, ok := s.danceScene("bd-0000000000"); ok {
		t.Error("an unknown clip should not load")
	}
}

func TestPhoneCanWatchClipsAndShareItsView(t *testing.T) {
	s, id := danceServer(t)
	s.comp = companion{on: true, code: "424242", port: "7443"}
	h := s.companionHandler()
	do := func(method, path, body, remote string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if remote != "" {
			r.RemoteAddr = remote
		}
		h.ServeHTTP(w, r)
		return w
	}
	if w := do("GET", "/dance.js", "", ""); w.Code != 200 || w.Body.Len() < 500 {
		t.Errorf("the phone page cannot load /dance.js: %d", w.Code)
	}
	for _, p := range []string{"/dance", "/dance/clip?id=" + id} {
		if w := do("GET", p, "", ""); w.Code != 403 {
			t.Errorf("%s without a code: %d", p, w.Code)
		}
	}
	var list struct {
		Clips []braindance.Clip `json:"clips"`
	}
	if w := do("GET", "/dance?code=424242", "", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Clips) == 0 || list.Clips[0].Samples != nil {
		t.Errorf("/dance should list clips without their samples: %d %s", w.Code, w.Body.String())
	}
	var scene struct {
		Samples []any `json:"samples"`
		Est     []any `json:"est"`
		Walls   []any `json:"walls"`
	}
	if w := do("GET", "/dance/clip?code=424242&id="+id, "", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &scene) != nil || len(scene.Samples) != 50 || len(scene.Est) != 50 || len(scene.Walls) != 1 {
		t.Errorf("/dance/clip: %d, %d samples", w.Code, len(scene.Samples))
	}
	if w := do("GET", "/dance/clip?code=424242&id=../../etc/passwd", "", ""); w.Code != 404 {
		t.Errorf("a made-up clip id: %d", w.Code)
	}

	// sharing the view
	frame := `{"x":0.2,"y":0.9,"h":0,"p":3,"m":"find","g":"aa:bb:cc:00:00:02","s":[{"l":"<img src=x>","k":"host","x":0,"y":0}]}`
	if w := do("POST", "/view", frame, "192.168.1.77:50000"); w.Code != 403 {
		t.Errorf("/view without a code: %d", w.Code)
	}
	if w := do("GET", "/view?code=424242", "", "192.168.1.77:50000"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("/view only takes POST: %d", w.Code)
	}
	if w := do("POST", "/view?code=424242", "not json", "192.168.1.77:50000"); w.Code != 400 {
		t.Errorf("a bad frame: %d", w.Code)
	}
	if w := do("POST", "/view?code=424242", `{"x":0,"pad":"`+strings.Repeat("a", 2000)+`"}`, "192.168.1.77:50000"); w.Code != 400 {
		t.Errorf("an oversized frame: %d", w.Code)
	}
	if w := do("POST", "/view?code=424242", frame, "192.168.1.77:50000"); w.Code != 200 || !strings.Contains(w.Body.String(), `"kept":true`) {
		t.Fatalf("/view with the code: %d %s", w.Code, w.Body.String())
	}
	ps := s.opt.Phones(time.Now())
	if len(ps) != 1 || ps[0].Phone != "192.168.1.77" || ps[0].Name != "my-phone" || !ps[0].Live {
		t.Fatalf("the console should list the phone by the name the watch knows it by: %+v", ps)
	}
	f := s.opt.Trail("192.168.1.77", 0)[0]
	if f.Mode != "find" || f.Target != "" {
		t.Errorf("a target the sensor nodes have not placed should be dropped: %+v", f)
	}
	for _, sn := range f.Seen { // what was in view comes from the console's own map, never from the phone
		if strings.Contains(sn.Label, "<") {
			t.Errorf("a label supplied by the phone was kept: %q", sn.Label)
		}
	}
	if len(f.Seen) == 0 || f.Seen[0].Label != "ACCESS POINT" {
		t.Errorf("facing up the map from below the access point should see it: %+v", f.Seen)
	}

	// the console side
	w := httptest.NewRecorder()
	s.handleOpticTrail(w, httptest.NewRequest("GET", "/api/optic/trail?phone=192.168.1.77", nil))
	var tr struct {
		Frames []any          `json:"frames"`
		Layout map[string]any `json:"layout"`
		FOV    float64        `json:"fov"`
	}
	if json.Unmarshal(w.Body.Bytes(), &tr) != nil || len(tr.Frames) != 1 || tr.Layout["@ap"] == nil || tr.FOV != 62 {
		t.Errorf("trail: %s", w.Body.String())
	}
	if r := call(t, s.handleOptic, `{"forget":"192.168.1.77"}`); r["ok"] != true || len(s.opt.Phones(time.Now())) != 0 {
		t.Errorf("forget did not drop the phone view: %v", r)
	}
}

func TestRepoOfFindsTheCheckout(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	exe := filepath.Join(dir, "bin", "netwatch")
	if got := repoOf(exe); got != "" {
		t.Errorf("without update.sh this is not a checkout, got %q", got)
	}
	os.WriteFile(filepath.Join(dir, "update.sh"), []byte("#!/bin/sh\n"), 0o755)
	if got := repoOf(exe); got != dir {
		t.Errorf("repoOf = %q, want %q", got, dir)
	}
	if got := repoOf("/usr/local/sbin/netwatch"); got != "" {
		t.Errorf("an installed binary has no checkout, got %q", got)
	}
}
