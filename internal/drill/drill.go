// Package drill is NetWatch's detector fire drill: it stages harmless,
// synthetic events against fresh copies of each detector and reports which
// ones responded. It proves the detection logic in this build still works; it
// does not exercise packet capture or the hardware sensors.
package drill

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/r0n1n9/netwatch/internal/anomaly"
	"github.com/r0n1n9/netwatch/internal/hostwatch"
	"github.com/r0n1n9/netwatch/internal/ice"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/passive"
	"github.com/r0n1n9/netwatch/internal/sonar"
)

// Result is the outcome for one detector.
type Result struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Run stages every drill and returns the results in a fixed order.
func Run() []Result {
	return []Result{
		check("ICE · new device", newDevice),
		check("ICE · gateway MAC change", arpSpoof),
		check("DNS · generated domain", dga),
		check("DNS · tunnelling", tunnel),
		check("Traffic · beaconing", beacon),
		check("TLS · JA3 fingerprint", ja3),
		check("Behaviour · anomaly model", outlier),
		check("SONAR · movement", movement),
		check("SONAR · breathing", breathing),
		check("SONAR · tripwire crossing", tripwire),
		check("USB · keystroke injection", keystrokes),
	}
}

// Lines renders results for the ops consoles.
func Lines(rs []Result) []string {
	pass := 0
	for _, r := range rs {
		if r.OK {
			pass++
		}
	}
	lines := []string{fmt.Sprintf("◆ DETECTOR FIRE DRILL · %d/%d responded · synthetic events, detection logic only", pass, len(rs))}
	for _, r := range rs {
		mark := "✓"
		if !r.OK {
			mark = "✗ FAILED"
		}
		lines = append(lines, fmt.Sprintf("%-8s %-28s %s", mark, r.Name, r.Detail))
	}
	return lines
}

func check(name string, fn func() (bool, string)) (r Result) {
	r.Name = name
	defer func() {
		if p := recover(); p != nil {
			r.OK, r.Detail = false, fmt.Sprint("panicked: ", p)
		}
	}()
	r.OK, r.Detail = fn()
	return r
}

func dev(ip, mac string, gw bool) model.Device {
	m, _ := net.ParseMAC(mac)
	return model.Device{IP: net.ParseIP(ip).To4(), MAC: m, IsGateway: gw}
}

func hasKind(alerts []model.Alert, kind string) bool {
	for _, a := range alerts {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

func newDevice() (bool, string) {
	e := ice.New()
	base := []model.Device{dev("10.99.0.1", "02:00:00:00:00:01", true)}
	e.Observe(base, nil)
	alerts := e.Observe(append(base, dev("10.99.0.66", "02:00:00:00:00:66", false)), nil)
	return hasKind(alerts, "new-device"), "a host joined between two sweeps"
}

func arpSpoof() (bool, string) {
	e := ice.New()
	e.Observe([]model.Device{dev("10.99.0.1", "02:00:00:00:00:01", true)}, nil)
	alerts := e.Observe([]model.Device{dev("10.99.0.1", "02:00:00:00:00:99", true)}, nil)
	return hasKind(alerts, "arp-spoof"), "the gateway answered from a different MAC"
}

func dga() (bool, string) {
	v := passive.ScoreDNS("kq3x9zrvbnmwptql.example", "A")
	return v.DGA >= 0.6, fmt.Sprintf("random-looking name scored %.2f", v.DGA)
}

func tunnel() (bool, string) {
	v := passive.ScoreDNS("ZXhmaWx0cmF0ZWRkYXRhYmxvYjEyMzQ1Njc4OWFiY2RlZmdoaWo.drill.example", "TXT")
	return v.Tunnel, "long encoded label in a TXT query"
}

func beacon() (bool, string) {
	v := passive.AnalyzeBeacon([]float64{60, 61, 59, 60, 60, 61})
	return v.Beacon, fmt.Sprintf("six callbacks ~60 s apart (jitter %.2f)", v.CV)
}

func ja3() (bool, string) {
	var b []byte
	b = append(b, 0x16, 0x03, 0x01, 0, 0, 0x01, 0, 0, 0, 0x03, 0x03)
	b = append(b, make([]byte, 32)...)
	b = append(b, 0x00, 0x00, 0x04, 0xc0, 0x2b, 0xc0, 0x2f, 0x01, 0x00)
	ext := []byte{0x00, 0x0a, 0x00, 0x04, 0x00, 0x02, 0x00, 0x1d}
	l := make([]byte, 2)
	binary.BigEndian.PutUint16(l, uint16(len(ext)))
	b = append(append(b, l...), ext...)
	s, hash, _, ok := passive.JA3(b)
	return ok && s == "771,49195-49199,10,29," && len(hash) == 32, "parsed a sample TLS hello"
}

func outlier() (bool, string) {
	m := anomaly.New()
	for _, v := range []float64{900, 1100, 950, 1050, 1000, 980, 1020, 1010} {
		m.Observe("drill", map[string]float64{"bps": v})
	}
	score, _ := m.Score("drill", map[string]float64{"bps": 60000})
	return score >= 40, fmt.Sprintf("a 60x traffic spike scored %.0f", score)
}

func feed(e *sonar.Engine, sim *sonar.Sim, now *time.Time, frames int, motion float64) {
	for i := 0; i < frames; i++ {
		*now = now.Add(40 * time.Millisecond)
		e.Ingest(sim.Frame(motion), *now)
	}
}

func movement() (bool, string) {
	e, sim, now := sonar.New(), sonar.NewSim("drill", 7), time.Unix(1_700_000_000, 0)
	fired := false
	e.OnMotion = func(string, float64) { fired = true }
	feed(e, sim, &now, 160, 0)
	feed(e, sim, &now, 50, 1)
	return fired, "simulated walk through a link"
}

func breathing() (bool, string) {
	e, sim, now := sonar.New(), sonar.NewSim("drill", 8), time.Unix(1_700_000_000, 0)
	sim.BreathBPM = 15
	feed(e, sim, &now, 25*40, 0)
	b := e.Snapshot(now)[0].Breath
	return b >= 13 && b <= 17, fmt.Sprintf("simulated 15/min breather read as %.0f/min", b)
}

func tripwire() (bool, string) {
	w := sonar.Wire{A: sonar.Pt{X: 0, Y: -1}, B: sonar.Pt{X: 0, Y: 1}}
	return sonar.Crossed(sonar.Pt{X: -0.5}, sonar.Pt{X: 0.5}, w) && !sonar.Crossed(sonar.Pt{X: 0.2}, sonar.Pt{X: 0.5}, w), "a path across a line, and one beside it"
}

func keystrokes() (bool, string) {
	start := time.Unix(1_700_000_000, 0)
	var fast []time.Time
	for i := 0; i < 60; i++ {
		fast = append(fast, start.Add(time.Duration(i)*8*time.Millisecond))
	}
	return hostwatch.TooFast(fast), "60 key presses in half a second"
}
