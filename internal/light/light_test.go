package light

import (
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestColourAndSend(t *testing.T) {
	cases := []struct {
		s     State
		pulse string
		red   bool
	}{
		{State{FreshMS: -1}, "steady", false},
		{State{Warnings: 2, FreshMS: 60000}, "steady", true},
		{State{Critical: 1, FreshMS: 2000}, "flash", true},
		{State{Critical: 1, FreshMS: 600000, Armed: true}, "breathe", true},
		{State{Armed: true, Away: true, FreshMS: -1}, "breathe", false},
	}
	for _, c := range cases {
		got := For(c.s)
		if got.Pulse != c.pulse || (got.R > 200) != c.red || got.Why == "" {
			t.Errorf("For(%+v) = %+v", c.s, got)
		}
	}
	for _, bad := range []string{"8.8.8.8:5577", "light.local:5577", "192.168.1.60"} {
		if ValidAddr(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback UDP here")
	}
	defer pc.Close()
	if err := Send(pc.LocalAddr().String(), For(State{Critical: 1, FreshMS: 100})); err != nil {
		t.Fatal(err)
	}
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _, err := pc.ReadFrom(buf)
	var got Color
	if err != nil || json.Unmarshal(buf[:n], &got) != nil || got.R != 255 || got.Pulse != "flash" {
		t.Errorf("the light received %q (%v)", buf[:n], err)
	}
}
