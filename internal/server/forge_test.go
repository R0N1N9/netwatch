package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestForgeDemoTouchesNoBoard(t *testing.T) {
	s := demoServer(t)
	v := getJSON(t, s.handleForge, "/api/forge")
	if len(v["boards"].([]any)) != 1 || v["demo"] != true || v["listening"] != "" {
		t.Fatalf("demo forge view = %v", v)
	}
	if r := call(t, s.handleForge, `{"flash":"/dev/ttyACM0","kind":"node","ssid":"","pass":"x"}`); r["ok"] != false {
		t.Error("flashed without a Wi-Fi name")
	}
	if r := call(t, s.handleForge, `{"flash":"/dev/ttyACM0","kind":"toaster","ssid":"home"}`); r["ok"] != false {
		t.Error("accepted an unknown kind of board")
	}
	if r := call(t, s.handleForge, `{"flash":"/dev/ttyACM0","kind":"node","ssid":"home","pass":"correct horse"}`); r["ok"] != true {
		t.Fatalf("demo flash: %v", r)
	}
	if r := call(t, s.handleForge, `{"flash":"/dev/ttyACM0","kind":"node","ssid":"home"}`); r["ok"] != false {
		t.Error("two flashes at once")
	}
	var job map[string]any
	for end := time.Now().Add(8 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		v = getJSON(t, s.handleForge, "/api/forge")
		if job = v["job"].(map[string]any); job["done"] == true {
			break
		}
	}
	if job["ok"] != true || len(job["lines"].([]any)) < 6 || v["listening"] != "" {
		t.Errorf("demo job = %v, listening = %v", job, v["listening"])
	}
	body := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(toJSON(v), "\n", " "), "\t", " "))), " "))
	if strings.Contains(body, "correct horse") || v["havePass"] != true || v["ssid"] != "home" {
		t.Errorf("the password must stay inside the server; view = %s", body)
	}
	if r := call(t, s.handleForge, `{"listen":true}`); r["ok"] != false {
		t.Error("demo mode opened a listening port")
	}
}

func TestForgeRefusesAPortThatIsNotABoard(t *testing.T) {
	s := demoServer(t)
	s.demo = false
	if r := call(t, s.handleForge, `{"flash":"/etc/passwd","kind":"node","ssid":"home","pass":"x"}`); r["ok"] != false || !strings.Contains(r["error"].(string), "not a board") {
		t.Errorf("flash to an arbitrary path: %v", r)
	}
}

func TestNodesAreAnsweredAndHeardWithoutAFlag(t *testing.T) {
	s := demoServer(t)
	s.demo = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.fg.ctx = ctx
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback UDP here")
	}
	addr := pc.LocalAddr().String()
	pc.Close()
	s.listenForNodes(addr)
	node, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	node.SetDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	var n int
	for i := 0; i < 20 && n == 0; i++ { // the listener may need a moment to open
		node.Write([]byte(`{"hello":"node-a1b2"}`))
		node.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		n, _ = node.Read(buf)
	}
	if string(buf[:n]) != `{"netwatch":1}` {
		t.Fatalf("a node asking where NetWatch is got %q", buf[:n])
	}
	amp := strings.TrimSuffix(strings.Repeat("12.5,", 52), ",")
	node.Write([]byte(`{"node":"node-a1b2","rssi":-50,"amp":[` + amp + `]}`))
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if nodes := s.son.Snapshot(time.Now()); len(nodes) == 1 && nodes[0].Node == "node-a1b2" {
			if v := getJSON(t, s.handleForge, "/api/forge"); v["listening"] != addr {
				t.Errorf("listening = %v", v["listening"])
			}
			return
		}
	}
	t.Error("the node's frame was not heard")
}
