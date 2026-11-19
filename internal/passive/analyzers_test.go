package passive

import (
	"encoding/binary"
	"testing"
)

func TestFingerprintSYN(t *testing.T) {
	cases := []struct {
		ttl        uint8
		win, mss   uint16
		wantPrefix string
	}{
		{64, 29200, 1460, "Linux"},
		{64, 65535, 1460, "macOS"},
		{128, 8192, 1460, "Windows"},
		{128, 64240, 1460, "Windows"},
		{255, 4128, 1460, "Network"},
	}
	for _, c := range cases {
		g := FingerprintSYN(c.ttl, c.win, c.mss)
		if len(g.OS) < len(c.wantPrefix) || g.OS[:len(c.wantPrefix)] != c.wantPrefix {
			t.Errorf("ttl=%d win=%d => %q, want prefix %q", c.ttl, c.win, g.OS, c.wantPrefix)
		}
	}
}

func buildClientHello() []byte {
	var b []byte
	b = append(b, 0x16, 0x03, 0x01, 0x00, 0x00) // record header
	b = append(b, 0x01, 0x00, 0x00, 0x00)       // handshake type + len
	b = append(b, 0x03, 0x03)                   // client_version @9 (771)
	b = append(b, make([]byte, 32)...)          // random
	b = append(b, 0x00)                         // session id len
	// cipher suites: len=4, c02b c02f
	b = append(b, 0x00, 0x04, 0xc0, 0x2b, 0xc0, 0x2f)
	// compression: len=1, null
	b = append(b, 0x01, 0x00)
	// extensions
	ext := func(typ uint16, data []byte) []byte {
		h := make([]byte, 4)
		binary.BigEndian.PutUint16(h, typ)
		binary.BigEndian.PutUint16(h[2:], uint16(len(data)))
		return append(h, data...)
	}
	sni := []byte{0x00, 0x08, 0x00, 0x00, 0x05, 'a', '.', 'c', 'o', 'm'}
	groups := []byte{0x00, 0x02, 0x00, 0x1d} // x25519 (29)
	formats := []byte{0x01, 0x00}
	var exts []byte
	exts = append(exts, ext(0x0000, sni)...)
	exts = append(exts, ext(0x000a, groups)...)
	exts = append(exts, ext(0x000b, formats)...)
	el := make([]byte, 2)
	binary.BigEndian.PutUint16(el, uint16(len(exts)))
	b = append(b, el...)
	b = append(b, exts...)
	return b
}

func TestJA3(t *testing.T) {
	b := buildClientHello()
	ja3, hash, sni, ok := JA3(b)
	if !ok {
		t.Fatal("JA3 failed to parse")
	}
	want := "771,49195-49199,0-10-11,29,0"
	if ja3 != want {
		t.Errorf("ja3 = %q, want %q", ja3, want)
	}
	if sni != "a.com" {
		t.Errorf("sni = %q, want a.com", sni)
	}
	if len(hash) != 32 {
		t.Errorf("hash len = %d, want 32", len(hash))
	}
	if !IsClientHello(b) {
		t.Error("IsClientHello = false")
	}
}

func TestScoreDNS(t *testing.T) {
	good := ScoreDNS("google.com", "A")
	if good.DGA > 0.4 {
		t.Errorf("google.com DGA = %.2f, want low", good.DGA)
	}
	bad := ScoreDNS("kq3x9zrvbnmwptql.com", "A")
	if bad.DGA < 0.5 {
		t.Errorf("random domain DGA = %.2f, want high", bad.DGA)
	}
	tun := ScoreDNS("ZXhmaWx0cmF0ZWRkYXRhYmxvYjEyMzQ1Njc4OWFiY2RlZmdoaWo.evil.com", "TXT")
	if !tun.Tunnel {
		t.Error("long TXT query not flagged as tunnel")
	}
}

func TestAnalyzeBeacon(t *testing.T) {
	regular := AnalyzeBeacon([]float64{60, 61, 59, 60, 60, 61})
	if !regular.Beacon {
		t.Errorf("regular intervals not flagged (CV=%.3f)", regular.CV)
	}
	jittery := AnalyzeBeacon([]float64{5, 90, 12, 300, 7, 220})
	if jittery.Beacon {
		t.Errorf("jittery intervals flagged as beacon (CV=%.3f)", jittery.CV)
	}
	if AnalyzeBeacon([]float64{10, 10}).Beacon {
		t.Error("too few samples flagged")
	}
}
