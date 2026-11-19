package copilot

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/r0n1n9/netwatch/internal/model"
)

func sampleContext() Context {
	return Context{
		Iface: "wlan0", Subnet: "192.168.1.0/24",
		Devices: []model.Device{
			{IP: net.ParseIP("192.168.1.1").To4(), Hostname: "gateway.lan", IsGateway: true},
			{IP: net.ParseIP("192.168.1.99").To4(), Vendor: "(randomized MAC)"},
		},
		Intel: map[string]HostIntel{"192.168.1.99": {OS: "macOS / iOS / BSD", Anomaly: 86, Beacon: "185.220.101.44:443 (~60s)"}},
	}
}

func TestOllamaDetectAndAsk(t *testing.T) {
	var gotModel, gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"llama3.2:3b"},{"name":"other"}]}`))
		case "/api/chat":
			var req struct {
				Model    string `json:"model"`
				Stream   bool   `json:"stream"`
				Messages []struct{ Role, Content string }
			}
			json.NewDecoder(r.Body).Decode(&req)
			gotModel, gotUser = req.Model, req.Messages[1].Content
			w.Write([]byte(`{"message":{"role":"assistant","content":" 192.168.1.99 is beaconing. Quarantine it. "}}`))
		}
	}))
	defer srv.Close()
	t.Setenv("NETWATCH_LLM_URL", srv.URL)
	t.Setenv("NETWATCH_LLM", "")
	t.Setenv("NETWATCH_LLM_MODEL", "")

	c := Detect(context.Background())
	if !strings.Contains(c.Name(), "llama3.2:3b") {
		t.Fatalf("backend = %q, want the first installed model", c.Name())
	}
	ans := c.Ask(context.Background(), "what's weird?", sampleContext())
	if ans != "192.168.1.99 is beaconing. Quarantine it." {
		t.Errorf("answer = %q", ans)
	}
	if gotModel != "llama3.2:3b" {
		t.Errorf("model sent = %q", gotModel)
	}
	for _, want := range []string{`"beacon":"185.220.101.44:443 (~60s)"`, `"role":"gateway"`, "QUESTION: what's weird?"} {
		if !strings.Contains(gotUser, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestDetectFallsBack(t *testing.T) {
	t.Setenv("NETWATCH_LLM_URL", "http://127.0.0.1:1") // nothing listening
	t.Setenv("NETWATCH_LLM", "")
	if c := Detect(context.Background()); c.Name() != "built-in rules" {
		t.Errorf("backend = %q, want built-in rules", c.Name())
	}
	t.Setenv("NETWATCH_LLM", "off")
	if c := Detect(context.Background()); c.Name() != "built-in rules" {
		t.Errorf("NETWATCH_LLM=off backend = %q", c.Name())
	}
}

func TestOllamaFailureUsesRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":"model not loaded"}`))
	}))
	defer srv.Close()
	o := &Ollama{URL: srv.URL, Model: "m", HTTP: srv.Client(), Fallback: Default()}
	ans := o.Ask(context.Background(), "status", sampleContext())
	if !strings.Contains(ans, "built-in rules") || !strings.Contains(ans, "hosts") {
		t.Errorf("fallback answer = %q", ans)
	}
}
