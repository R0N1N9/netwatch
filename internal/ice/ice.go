// Package ice is the seed of the ICE (Intrusion Countermeasures) engine. For
// now it derives alerts by diffing successive sweeps; passive packet-level
// detectors (ARP-spoof live, port-scan, rogue DHCP) plug in here next.
package ice

import (
	"fmt"
	"net"
	"time"

	"github.com/r0n1n9/netwatch/internal/model"
)

// Engine holds state across sweeps so it can reason about change.
type Engine struct {
	known      map[string]model.Device // keyed by IP
	gatewayMAC string
	started    bool
}

// New returns an empty engine.
func New() *Engine {
	return &Engine{known: map[string]model.Device{}}
}

// Observe folds a fresh sweep into engine state and returns any new alerts.
// trusted is the set of IPs the operator has marked known-good.
func (e *Engine) Observe(devices []model.Device, trusted map[string]bool) []model.Alert {
	now := time.Now()
	var alerts []model.Alert

	for _, d := range devices {
		key := d.IP.String()

		// Gateway MAC change across sweeps == classic ARP-spoof / MITM signal.
		if d.IsGateway {
			mac := macKey(d.MAC)
			if e.gatewayMAC != "" && mac != "" && mac != e.gatewayMAC {
				alerts = append(alerts, model.Alert{
					Time:     now,
					Severity: model.SevCritical,
					Kind:     "arp-spoof",
					Source:   d.IP.String(),
					Message:  fmt.Sprintf("gateway MAC changed %s → %s (possible MITM)", e.gatewayMAC, mac),
				})
			}
			if mac != "" {
				e.gatewayMAC = mac
			}
		}

		if _, seen := e.known[key]; !seen && e.started {
			sev := model.SevWarn
			if trusted[key] {
				sev = model.SevInfo
			}
			msg := fmt.Sprintf("new host joined the segment: %s", label(d))
			if d.Vendor == "(randomized MAC)" {
				msg += " · randomized MAC (privacy address or evasion)"
			}
			alerts = append(alerts, model.Alert{
				Time:     now,
				Severity: sev,
				Kind:     "new-device",
				Source:   d.IP.String(),
				Message:  msg,
			})
		}
		e.known[key] = d
	}

	e.started = true
	return alerts
}

func label(d model.Device) string {
	parts := d.IP.String()
	if d.Hostname != "" {
		parts += " (" + d.Hostname + ")"
	}
	if d.Vendor != "" {
		parts += " · " + d.Vendor
	}
	return parts
}

func macKey(m net.HardwareAddr) string {
	if len(m) == 0 {
		return ""
	}
	return m.String()
}
