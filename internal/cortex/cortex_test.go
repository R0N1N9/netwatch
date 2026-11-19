package cortex

import (
	"strings"
	"testing"
)

func TestBeaconingStrangerIsAThreat(t *testing.T) {
	c := Investigate(Evidence{
		Kind: "beacon", Severity: "WARN", Source: "192.168.1.99", Message: "periodic beacon",
		Host:    &Host{IP: "192.168.1.99", Vendor: "(randomized MAC)", Role: "unverified", Anomaly: 86, Beacon: "185.220.101.44:443 (~60s)"},
		Related: []string{"new host joined the segment"},
	})
	if c.Verdict != "likely threat" || c.Proposal.Action != "quarantine" || c.Proposal.Target != "192.168.1.99" {
		t.Errorf("verdict=%q score=%d proposal=%+v", c.Verdict, c.Score, c.Proposal)
	}
	if len(c.For) < 3 || len(c.Against) < 1 || c.Confidence < 60 {
		t.Errorf("for=%d against=%d confidence=%d", len(c.For), len(c.Against), c.Confidence)
	}
}

func TestGatewayIsVerifiedNeverQuarantined(t *testing.T) {
	c := Investigate(Evidence{Kind: "arp-spoof", Severity: "CRITICAL", Source: "192.168.1.1",
		Host: &Host{IP: "192.168.1.1", Name: "gateway.lan", Vendor: "Ubiquiti", Role: "gateway"}})
	if c.Proposal.Action != "verify" || !strings.Contains(c.Proposal.Why, "Never quarantine the gateway") {
		t.Errorf("proposal = %+v", c.Proposal)
	}
}

func TestNewNamedGadgetIsBenign(t *testing.T) {
	c := Investigate(Evidence{Kind: "new-device", Severity: "INFO", Source: "192.168.1.31",
		Host: &Host{IP: "192.168.1.31", Name: "echo-dot", Vendor: "Amazon", Role: "unverified", Anomaly: 12}})
	if c.Verdict != "likely benign" || c.Proposal.Action != "trust" {
		t.Errorf("verdict=%q score=%d proposal=%+v", c.Verdict, c.Score, c.Proposal)
	}
}

func TestTrustedHostAndNonHostSources(t *testing.T) {
	c := Investigate(Evidence{Kind: "anomaly", Source: "192.168.1.20", Host: &Host{IP: "192.168.1.20", Name: "pi-hole.lan", Vendor: "Raspberry Pi", Role: "trusted"}})
	if c.Verdict == "likely threat" || c.Proposal.Action == "quarantine" {
		t.Errorf("a trusted host with one anomaly: %q %+v", c.Verdict, c.Proposal)
	}
	c = Investigate(Evidence{Kind: "tripwire", Source: "tripwire:hall"})
	if c.Proposal.Action != "watch" {
		t.Errorf("non-host source proposal = %+v", c.Proposal)
	}
	c = Investigate(Evidence{Kind: "decoy", Source: "192.168.1.99", DecoyTrips: 2, Host: &Host{IP: "192.168.1.99", Role: "unverified"}})
	if c.Verdict != "likely threat" {
		t.Errorf("decoy toucher verdict = %q (score %d)", c.Verdict, c.Score)
	}
}

func TestADrillIsNotAThreat(t *testing.T) {
	c := Investigate(Evidence{Kind: "decoy-drill", Severity: "INFO", Source: "127.0.0.1", DecoyTrips: 3})
	if c.Verdict != "likely benign" || c.Proposal.Action != "watch" {
		t.Errorf("a drill was judged %q with proposal %+v", c.Verdict, c.Proposal)
	}
}
