package audit

import "testing"

func TestParseTCPLine(t *testing.T) {
	est := "   3: 0200A8C0:D6F4 197171E9:01BB 01 00000000:00000000 02:000A7B3C 00000000  1000        0 48213 2 0000000000000000 20 4 30 10 -1"
	remote, inode, ok := parseTCPLine(est)
	if !ok || remote != "233.113.113.25:443" || inode != 48213 {
		t.Fatalf("got %q inode=%d ok=%v", remote, inode, ok)
	}
	listen := "   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1234 1 0000000000000000 100 0 0 10 0"
	if _, _, ok := parseTCPLine(listen); ok {
		t.Error("LISTEN row treated as an established connection")
	}
	if _, _, ok := parseTCPLine("garbage"); ok {
		t.Error("garbage accepted")
	}
}
