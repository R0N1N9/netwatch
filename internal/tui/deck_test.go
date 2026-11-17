package tui

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/netif"
)

func sampleInfo() *netif.Info {
	_, ipnet, _ := net.ParseCIDR("192.168.1.0/24")
	return &netif.Info{
		Iface:   &net.Interface{Index: 1, Name: "eth0", HardwareAddr: net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}},
		IP:      net.ParseIP("192.168.1.50").To4(),
		IPNet:   ipnet,
		Gateway: net.ParseIP("192.168.1.1").To4(),
	}
}

func dev(ip, mac, vendor, host string, self, gw bool) model.Device {
	m, _ := net.ParseMAC(mac)
	return model.Device{
		IP: net.ParseIP(ip).To4(), MAC: m, Vendor: vendor, Hostname: host,
		FirstSeen: time.Now(), LastSeen: time.Now(), IsSelf: self, IsGateway: gw,
	}
}

func apply(m tea.Model, msg tea.Msg) tea.Model {
	next, _ := m.Update(msg)
	return next
}

func TestDeckRenders(t *testing.T) {
	var m tea.Model = New(sampleInfo(), 0)
	m = apply(m, tea.WindowSizeMsg{Width: 96, Height: 30})

	first := []model.Device{
		dev("192.168.1.1", "f0:27:2d:aa:bb:cc", "Ubiquiti", "gateway.lan", false, true),
		dev("192.168.1.50", "de:ad:be:ef:00:01", "VirtualBox", "", true, false),
		dev("192.168.1.20", "b8:27:eb:11:22:33", "Raspberry Pi", "pi-hole.lan", false, false),
	}
	m = apply(m, sweepDoneMsg{devices: first})

	// Second sweep: a new unknown host joins -> should raise a new-device alert.
	second := append([]model.Device{
		dev("192.168.1.99", "02:11:22:33:44:55", "(randomized MAC)", "", false, false),
	}, first...)
	m = apply(m, sweepDoneMsg{devices: second})

	view := m.(Model).View()
	if view == "" {
		t.Fatal("empty view")
	}
	for _, want := range []string{"NETWATCH", "DEVICES", "ALERTS", "STATS", "192.168.1.99"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if os.Getenv("SHOW_DECK") != "" {
		os.Stdout.WriteString("\n" + view + "\n")
	}
}
