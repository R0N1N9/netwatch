// Package light decides what colour an ambient threat light should show and
// sends it to an ESP32 driving an LED strip, as one small UDP datagram.
package light

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// State is what the light reflects.
type State struct {
	Critical  int   // critical alerts nobody has opened
	Warnings  int   // warnings nobody has opened
	FreshMS   int64 // age of the newest unopened alert, in milliseconds (-1 = none)
	Armed     bool  // SONAR is armed
	Away      bool  // nobody is home
	Rehearsal bool
	Flash     bool // a scene asked for attention
}

// Color is one frame for the strip.
type Color struct {
	R     uint8  `json:"r"`
	G     uint8  `json:"g"`
	B     uint8  `json:"b"`
	Pulse string `json:"pulse"` // steady | breathe | flash
	Why   string `json:"why"`
}

// For picks the colour for a state. Worst news wins.
func For(s State) Color {
	switch {
	case s.Flash:
		return Color{255, 255, 255, "flash", "a scene fired"}
	case s.Critical > 0 && s.FreshMS >= 0 && s.FreshMS < 15000:
		return Color{255, 20, 10, "flash", "a critical alert just arrived"}
	case s.Critical > 0:
		return Color{255, 30, 20, "breathe", fmt.Sprintf("%d critical alert(s) unopened", s.Critical)}
	case s.Rehearsal:
		return Color{255, 150, 0, "breathe", "a rehearsal is running"}
	case s.Warnings > 0:
		return Color{255, 140, 0, "steady", fmt.Sprintf("%d warning(s) unopened", s.Warnings)}
	case s.Armed && s.Away:
		return Color{40, 60, 255, "breathe", "armed and nobody home"}
	case s.Armed:
		return Color{40, 60, 255, "steady", "SONAR armed"}
	default:
		return Color{0, 200, 110, "steady", "all quiet"}
	}
}

// ValidAddr accepts host:port only for a private address: the light is on your own network.
func ValidAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("give the light's address as ip:port, for example 192.168.1.60:5577")
	}
	ip := net.ParseIP(host)
	if ip == nil || !(ip.IsPrivate() || ip.IsLoopback()) {
		return fmt.Errorf("the light must be at a private address on your own network")
	}
	return nil
}

// Send delivers one frame.
func Send(addr string, c Color) error {
	if err := ValidAddr(addr); err != nil {
		return err
	}
	conn, err := net.DialTimeout("udp", addr, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	b, _ := json.Marshal(c)
	_, err = conn.Write(b)
	return err
}
