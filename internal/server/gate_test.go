package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/netif"
)

func testServer(t *testing.T) *Server {
	t.Setenv("NETWATCH_LLM", "off")
	_, ipnet, _ := net.ParseCIDR("192.168.1.0/24")
	s := New(&netif.Info{Iface: &net.Interface{Name: "test0"}, IP: net.ParseIP("192.168.1.50").To4(), IPNet: ipnet}, 0, true)
	phone, _ := net.ParseMAC("aa:bb:cc:00:00:01")
	other, _ := net.ParseMAC("aa:bb:cc:00:00:02")
	s.devices = []model.Device{
		{IP: net.ParseIP("192.168.1.77").To4(), MAC: phone, Hostname: "my-phone"},
		{IP: net.ParseIP("192.168.1.31").To4(), MAC: other, Hostname: "echo-dot"},
	}
	return s
}

func call(t *testing.T, h http.HandlerFunc, body string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad response %q: %v", w.Body.String(), err)
	}
	return out
}

func TestPresenceGate(t *testing.T) {
	s := testServer(t)
	trust := `{"ip":"192.168.1.31","trusted":true}`

	if r := call(t, s.handleTrust, trust); r["ok"] != true {
		t.Fatalf("no gate set: trust refused: %v", r)
	}
	if r := call(t, s.handleGate, `{"ip":"10.9.9.9"}`); r["ok"] != false {
		t.Error("gate accepted a host that is not on the segment")
	}
	if r := call(t, s.handleGate, `{"ip":"192.168.1.77"}`); r["ok"] != true {
		t.Fatalf("could not set the key device: %v", r)
	}
	if r := call(t, s.handleTrust, trust); r["ok"] != true {
		t.Errorf("key present: trust refused: %v", r)
	}

	s.devices = s.devices[1:] // the phone leaves the network
	if r := call(t, s.handleTrust, trust); r["ok"] != false || !strings.Contains(r["error"].(string), "my-phone") {
		t.Errorf("key absent: trust was not refused: %v", r)
	}
	if r := call(t, s.handleBlackwall, `{"ip":"192.168.1.31","action":"quarantine"}`); r["ok"] != false {
		t.Errorf("key absent: quarantine was not refused: %v", r)
	}
	if r := call(t, s.handleGate, `{"ip":""}`); r["ok"] != false {
		t.Error("gate could be removed while the key device was absent")
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
