// Package model holds NetWatch's core domain types.
package model

import (
	"net"
	"time"
)

// Device is a host observed on the local network.
type Device struct {
	IP        net.IP
	MAC       net.HardwareAddr
	Vendor    string
	Hostname  string
	FirstSeen time.Time
	LastSeen  time.Time
	Trusted   bool   // marked known-good by the operator
	Note      string // operator-assigned nickname / note
	IsSelf    bool   // this machine
	IsGateway bool   // the default gateway
}

// Severity ranks an alert.
type Severity int

const (
	SevInfo Severity = iota
	SevWarn
	SevCritical
)

func (s Severity) String() string {
	switch s {
	case SevCritical:
		return "CRITICAL"
	case SevWarn:
		return "WARN"
	default:
		return "INFO"
	}
}

// Alert is a detection emitted by the ICE engine.
type Alert struct {
	Time     time.Time
	Severity Severity
	Kind     string // e.g. "arp-spoof", "new-device", "port-scan"
	Source   string // IP or MAC that triggered it
	Message  string
}
