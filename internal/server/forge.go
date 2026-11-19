package server

// The node forge: flash an ESP32-S3 that has been plugged in, hand it its
// settings, and start listening for it — from the console.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/r0n1n9/netwatch/internal/forge"
	"github.com/r0n1n9/netwatch/internal/model"
)

const sonarDefaultListen = "0.0.0.0:5566"

// forgeState is guarded by Server.mu.
type forgeState struct {
	ssid, pass string // kept in memory only, so five boards need typing it once
	job        *forgeJob
	listening  string // the UDP address NetWatch is accepting node frames on ("" = not listening)
	ctx        context.Context
}

type forgeJob struct {
	Port  string   `json:"port"`
	Kind  string   `json:"kind"`
	Lines []string `json:"lines"`
	Done  bool     `json:"done"`
	OK    bool     `json:"ok"`
	Error string   `json:"error"`

	Started      int64 `json:"started"`      // unix ms; tells one flash from the next on the same port
	SettingsOnly bool  `json:"settingsOnly"` // only the settings were written, not the firmware

	Stage    string `json:"stage"`    // connect | write | verify | restart
	Progress int    `json:"progress"` // percent written
	// After a sensor node is flashed NetWatch waits for it to report in.
	Online  string `json:"online"`  // the node's name once its first frames arrive
	WaitSec int    `json:"waitSec"` // how long NetWatch has been waiting for that
	Stalled bool   `json:"stalled"` // a minute has passed and it has not reported

	doneAt     time.Time
	liveBefore map[string]bool // nodes that were already reporting when the flash finished
}

// track reads a line of the flasher's output into the job's stage and progress. Caller holds s.mu.
func (j *forgeJob) track(line string) {
	stage, pct, ok := forge.Stage(line)
	if !ok {
		return
	}
	if stage == "write" && j.Stage == "write" && pct < j.Progress {
		return // the small settings file being written after the image: keep the bar where it was
	}
	j.Stage = stage
	if pct > j.Progress {
		j.Progress = pct
	}
}

// forgeWatch notices the freshly flashed node reporting in.
func (s *Server) forgeWatch() {
	nodes := s.son.Snapshot(time.Now())
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.fg.job
	if j == nil || !j.Done || !j.OK || j.Kind != "node" || j.Online != "" || j.doneAt.IsZero() {
		return
	}
	wait := time.Since(j.doneAt)
	j.WaitSec, j.Stalled = int(wait.Seconds()), wait > time.Minute
	if s.demo {
		if wait > 3*time.Second {
			j.Online = "node-a1b2 (simulated)"
		}
		return
	}
	for _, n := range nodes {
		if !j.liveBefore[n.Node] && n.AgeMS < 3000 {
			j.Online, j.Stalled = n.Node, false
			go s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevInfo, Kind: "forge", Source: n.Node, Message: "sensor node " + n.Node + " is online and reporting"})
			return
		}
	}
}

// listenForNodes starts accepting sensor frames on addr, unless NetWatch already is.
func (s *Server) listenForNodes(addr string) {
	s.mu.Lock()
	if s.fg.listening != "" || s.fg.ctx == nil {
		s.mu.Unlock()
		return
	}
	s.fg.listening = addr
	ctx := s.fg.ctx
	s.mu.Unlock()
	go func() {
		if err := s.son.ListenUDP(ctx, addr); err != nil {
			log.Printf("sonar: cannot listen on %s: %v", addr, err)
			s.mu.Lock()
			s.fg.listening = ""
			s.mu.Unlock()
		}
	}()
}

func (s *Server) forgeView() map[string]any {
	s.forgeWatch()
	boards := forge.Boards()
	_, toolName := forge.Tool()
	if s.demo {
		boards = []forge.Board{{Port: "/dev/ttyACM0", Chip: "USB Single Serial — WCH CH34x / CH9102 (UART port) · sample", VIDPID: "1a86:55d3"}}
		toolName = "esptool (sample)"
	}
	imgs := map[string]any{}
	for kind := range forge.Kinds {
		b, src, ok := forge.Image(kind, s.DataDir)
		imgs[kind] = map[string]any{"have": ok || s.demo, "source": src, "kb": len(b) / 1024}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"demo": s.demo, "boards": boards, "tool": toolName, "searched": forge.Searched(), "toolPath": forge.ToolPath, "build": buildStamp(), "images": imgs, "job": s.fg.job,
		"ssid": s.fg.ssid, "havePass": s.fg.pass != "", "listening": s.fg.listening, "host": s.info.IP.String(), "defaultListen": sonarDefaultListen}
}

func (s *Server) handleForge(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, s.forgeView())
		return
	}
	var req struct {
		Flash        string  `json:"flash"` // the port to flash
		Kind         string  `json:"kind"`  // node | light
		SSID         string  `json:"ssid"`
		Pass         string  `json:"pass"`
		Name         string  `json:"name"`
		SettingsOnly bool    `json:"settingsOnly"`
		Listen       *bool   `json:"listen"`
		ToolPath     *string `json:"toolPath"` // where esptool is, when NetWatch cannot find it
	}
	if !decode(w, r, &req) {
		return
	}
	fail := func(msg string) { writeJSON(w, map[string]any{"ok": false, "error": msg}) }
	if req.ToolPath != nil {
		path := strings.TrimSpace(*req.ToolPath)
		if path != "" {
			if err := forge.CheckTool(path); err != nil {
				fail(err.Error())
				return
			}
		}
		forge.ToolPath = path
		if !s.demo {
			s.saveModules()
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if req.Listen != nil {
		if s.demo {
			fail("demo mode does not open a port; the simulated nodes are already reporting")
			return
		}
		if *req.Listen {
			s.listenForNodes(sonarDefaultListen)
			s.logAction("forge", "started listening for sensor nodes on "+sonarDefaultListen)
		}
		s.saveModules()
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if _, ok := forge.Kinds[req.Kind]; !ok {
		fail("choose what to make of the board: a sensor node or a threat light")
		return
	}
	s.mu.Lock()
	if req.Pass == "" && req.SSID == s.fg.ssid {
		req.Pass = s.fg.pass // the password typed for an earlier board
	}
	busy := s.fg.job != nil && !s.fg.job.Done
	s.mu.Unlock()
	if busy {
		fail("a board is already being flashed")
		return
	}
	cfg, err := forge.ConfigBlob(forge.Settings{SSID: req.SSID, Pass: req.Pass, Host: s.info.IP.String(), Port: 5566, Name: strings.TrimSpace(req.Name)})
	if err != nil {
		fail(err.Error())
		return
	}
	job := &forgeJob{Port: req.Flash, Kind: req.Kind, Lines: []string{}, Started: time.Now().UnixMilli(), SettingsOnly: req.SettingsOnly, Stage: "connect"}
	add := func(line string) {
		s.mu.Lock()
		job.track(line)
		if n := len(job.Lines); n > 0 && strings.HasPrefix(line, "Writing at") && strings.HasPrefix(job.Lines[n-1], "Writing at") {
			job.Lines[n-1] = line // esptool redraws its progress line
		} else if n < 400 {
			job.Lines = append(job.Lines, line)
		}
		s.mu.Unlock()
	}
	finish := func(err error) {
		live := map[string]bool{}
		for _, n := range s.son.Snapshot(time.Now()) {
			if n.AgeMS < 3000 {
				live[n.Node] = true
			}
		}
		s.mu.Lock()
		job.Done, job.OK, job.doneAt, job.liveBefore = true, err == nil, time.Now(), live
		if err != nil {
			job.Error = err.Error()
		} else {
			job.Stage, job.Progress = "restart", 100
		}
		s.mu.Unlock()
	}
	if s.demo {
		s.mu.Lock()
		s.fg.ssid, s.fg.pass, s.fg.job = req.SSID, req.Pass, job
		s.mu.Unlock()
		go func() { // a pretend flash: nothing is run and nothing is written
			for _, l := range []string{"demo mode: no board is touched", "esptool.py v4.8.1", "Serial port " + req.Flash, "Connecting....", "Chip is ESP32-S3 (QFN56) (revision v0.2)",
				"Writing at 0x00010000... (8 %)", "Writing at 0x00028000... (21 %)", "Writing at 0x00040000... (34 %)", "Writing at 0x00058000... (47 %)", "Writing at 0x00070000... (60 %)",
				"Writing at 0x00088000... (73 %)", "Writing at 0x000a0000... (86 %)", "Writing at 0x000cc000... (100 %)", "Hash of data verified.", "Hard resetting via RTS pin...", "Done (simulated). On a real board this takes about twenty seconds."} {
				time.Sleep(420 * time.Millisecond)
				add(l)
			}
			finish(nil)
		}()
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	onUSB := false
	for _, b := range forge.Boards() {
		onUSB = onUSB || b.Port == req.Flash
	}
	if !onUSB {
		fail("that port is not a board NetWatch can see")
		return
	}
	tool, _ := forge.Tool()
	if tool == nil {
		fail("esptool is not installed — run: pipx install esptool   (or: sudo apt install esptool)")
		return
	}
	var image []byte
	if !req.SettingsOnly {
		img, _, ok := forge.Image(req.Kind, s.DataDir)
		if !ok {
			fail("this build of NetWatch has no firmware image for that — run git pull and make build, or put the .bin in the data directory's firmware folder")
			return
		}
		image = img
	}
	s.mu.Lock()
	s.fg.ssid, s.fg.pass, s.fg.job = req.SSID, req.Pass, job
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		what := map[string]string{"node": "sensor node", "light": "threat light"}[req.Kind]
		add(fmt.Sprintf("Making a %s of the board on %s (network \"%s\", reporting to %s:5566)", what, req.Flash, req.SSID, s.info.IP))
		err := forge.Flash(ctx, tool, req.Flash, image, cfg, add)
		if err != nil {
			add("Failed. If it could not connect: hold the board's BOOT button, tap RESET, release BOOT, and try again. If it says permission denied, add yourself to the dialout group and log in again.")
			finish(err)
			return
		}
		s.logAction("forge", fmt.Sprintf("flashed the board on %s as a %s", req.Flash, what))
		if req.Kind == "node" {
			s.listenForNodes(sonarDefaultListen) // so it shows up without restarting with a flag
			s.saveModules()
			add("Flashed. The board is restarting and joining \"" + req.SSID + "\" — NetWatch will say when it reports in. Then unplug it and power it anywhere in range.")
		} else {
			add("Done. Power it up; it logs its address on the serial port. Put that address (port 5577) in REFLEX → AMBIENT THREAT LIGHT.")
		}
		finish(nil)
	}()
	writeJSON(w, map[string]any{"ok": true})
}

// boardPluggedIn tells the operator a board has arrived and can be flashed.
func (s *Server) boardPluggedIn(vid string) {
	if vid != "303a" && vid != "10c4" && vid != "1a86" {
		return
	}
	s.emitAlert(model.Alert{Time: time.Now(), Severity: model.SevInfo, Kind: "forge", Source: "usb",
		Message: "a development board was plugged in — open SONAR → ADD A NODE to flash it as a sensor node"})
}

// buildStamp says which build this is, so an old binary still running is easy to spot.
func buildStamp() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	rev, when := "", ""
	for _, kv := range info.Settings {
		switch kv.Key {
		case "vcs.revision":
			if len(kv.Value) >= 7 {
				rev = kv.Value[:7]
			}
		case "vcs.time":
			when = kv.Value
		}
	}
	if rev == "" {
		return ""
	}
	return rev + " · " + when
}
