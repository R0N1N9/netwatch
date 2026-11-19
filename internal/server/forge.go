package server

// The node forge: flash an ESP32-S3 that has been plugged in, hand it its
// settings, and start listening for it — from the console.

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
	return map[string]any{"demo": s.demo, "boards": boards, "tool": toolName, "images": imgs, "job": s.fg.job,
		"ssid": s.fg.ssid, "havePass": s.fg.pass != "", "listening": s.fg.listening, "host": s.info.IP.String(), "defaultListen": sonarDefaultListen}
}

func (s *Server) handleForge(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, s.forgeView())
		return
	}
	var req struct {
		Flash        string `json:"flash"` // the port to flash
		Kind         string `json:"kind"`  // node | light
		SSID         string `json:"ssid"`
		Pass         string `json:"pass"`
		Name         string `json:"name"`
		SettingsOnly bool   `json:"settingsOnly"`
		Listen       *bool  `json:"listen"`
	}
	if !decode(w, r, &req) {
		return
	}
	fail := func(msg string) { writeJSON(w, map[string]any{"ok": false, "error": msg}) }
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
	job := &forgeJob{Port: req.Flash, Kind: req.Kind, Lines: []string{}}
	add := func(line string) {
		s.mu.Lock()
		if n := len(job.Lines); n > 0 && strings.HasPrefix(line, "Writing at") && strings.HasPrefix(job.Lines[n-1], "Writing at") {
			job.Lines[n-1] = line // esptool redraws its progress line
		} else if n < 400 {
			job.Lines = append(job.Lines, line)
		}
		s.mu.Unlock()
	}
	finish := func(err error) {
		s.mu.Lock()
		job.Done, job.OK = true, err == nil
		if err != nil {
			job.Error = err.Error()
		}
		s.mu.Unlock()
	}
	if s.demo {
		s.mu.Lock()
		s.fg.ssid, s.fg.pass, s.fg.job = req.SSID, req.Pass, job
		s.mu.Unlock()
		go func() { // a pretend flash: nothing is run and nothing is written
			for _, l := range []string{"demo mode: no board is touched", "esptool.py v4.8.1", "Serial port " + req.Flash, "Connecting....", "Chip is ESP32-S3 (QFN56) (revision v0.2)",
				"Writing at 0x00010000... (25 %)", "Writing at 0x00080000... (70 %)", "Writing at 0x000f0000... (100 %)", "Hash of data verified.", "Hard resetting via RTS pin...", "Done (simulated). On a real board this takes about twenty seconds."} {
				time.Sleep(350 * time.Millisecond)
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
			add("Done. Unplug it and power it anywhere in range of your Wi-Fi: it joins \"" + req.SSID + "\" and appears in SONAR within about ten seconds.")
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
