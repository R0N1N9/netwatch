package decoy

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func waitTrip(t *testing.T, ch chan Trip) Trip {
	t.Helper()
	select {
	case tr := <-ch:
		return tr
	case <-time.After(3 * time.Second):
		t.Fatal("no trip arrived")
		return Trip{}
	}
}

func TestHoneyport(t *testing.T) {
	ch := make(chan Trip, 4)
	m := New(func(tr Trip) { ch <- tr })
	ln, _ := net.Listen("tcp", "127.0.0.1:0") // find a free port
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err := m.openPort(ln.Addr().String(), port); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.openPort(ln.Addr().String(), port); err == nil {
		t.Error("the same port was opened twice")
	}
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("GET /admin HTTP/1.1\r\n\x00\x01"))
	c.Close()
	tr := waitTrip(t, ch)
	if tr.Kind != "honeyport" || tr.Source != "127.0.0.1" || !strings.Contains(tr.Detail, "GET /admin HTTP/1.1..") {
		t.Errorf("trip = %+v", tr)
	}
	if len(m.Trips()) != 1 || len(m.State().Ports) != 1 {
		t.Errorf("state = %+v, trips = %d", m.State(), len(m.Trips()))
	}
}

func TestCanaryFile(t *testing.T) {
	ch := make(chan Trip, 4)
	m := New(func(tr Trip) { ch <- tr })
	dir := t.TempDir()
	path, err := m.PlantFile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.PlantFile(dir, ""); err == nil {
		t.Error("planted over an existing canary")
	}
	if _, err := m.PlantFile(dir, "../escape.txt"); err == nil {
		t.Error("a name with a path was accepted")
	}
	time.Sleep(450 * time.Millisecond)
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	if tr := waitTrip(t, ch); tr.Kind != "canary-file" || tr.Decoy != path {
		t.Errorf("trip = %+v", tr)
	}
	m.RemoveFile(path)
	if _, err := os.Stat(path); err == nil {
		t.Error("the canary file was not removed")
	}
}

func TestCanaryName(t *testing.T) {
	ch := make(chan Trip, 4)
	m := New(func(tr Trip) { ch <- tr })
	m.AddName("Payroll-Backup.lan.")
	m.SeenDNS("192.168.1.31", "example.com")
	m.SeenDNS("192.168.1.99", "payroll-backup.lan.")
	tr := waitTrip(t, ch)
	if tr.Kind != "canary-name" || tr.Source != "192.168.1.99" || len(ch) != 0 {
		t.Errorf("trip = %+v, extra = %d", tr, len(ch))
	}
}
