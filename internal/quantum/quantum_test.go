package quantum

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func tlsServer(t *testing.T, cfg func(*tls.Config)) string {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{}
	cfg(srv.TLS)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func TestScanTLS(t *testing.T) {
	modern := tlsServer(t, func(c *tls.Config) {})
	f, ok := ScanTLS(context.Background(), modern)
	if !ok || f.Version != "TLS 1.3" || !f.PQ || !strings.HasPrefix(f.Key, "RSA") {
		t.Fatalf("modern server: ok=%v %+v", ok, f)
	}

	classical := tlsServer(t, func(c *tls.Config) { c.CurvePreferences = []tls.CurveID{tls.X25519} })
	f, ok = ScanTLS(context.Background(), classical)
	if !ok || f.PQ || !strings.Contains(strings.Join(f.Issues, ";"), "quantum-vulnerable") {
		t.Errorf("classical-only server: ok=%v %+v", ok, f)
	}

	old := tlsServer(t, func(c *tls.Config) { c.MaxVersion = tls.VersionTLS12 })
	f, ok = ScanTLS(context.Background(), old)
	if !ok || f.Version != "TLS 1.2" || f.PQ {
		t.Errorf("TLS 1.2 server: ok=%v %+v", ok, f)
	}

	if _, ok := ScanTLS(context.Background(), "8.8.8.8:443"); ok {
		t.Error("a public address was scanned")
	}
	if _, ok := ScanTLS(context.Background(), "127.0.0.1:1"); ok {
		t.Error("a closed port produced a finding")
	}
}

func fakeSSH(t *testing.T, kex string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			nameList := func(s string) []byte {
				b := make([]byte, 4)
				binary.BigEndian.PutUint32(b, uint32(len(s)))
				return append(b, s...)
			}
			payload := append([]byte{20}, make([]byte, 16)...)
			payload = append(payload, nameList(kex)...)
			payload = append(payload, nameList("ssh-ed25519,rsa-sha2-512")...)
			pad := byte(8)
			pkt := make([]byte, 5)
			binary.BigEndian.PutUint32(pkt, uint32(len(payload)+int(pad)+1))
			pkt[4] = pad
			c.Write([]byte("SSH-2.0-OpenSSH_9.9\r\n"))
			c.Write(append(append(pkt, payload...), make([]byte, pad)...))
			c.Close()
		}
	}()
	return ln.Addr().String()
}

func TestScanSSH(t *testing.T) {
	f, ok := ScanSSH(context.Background(), fakeSSH(t, "mlkem768x25519-sha256,curve25519-sha256"))
	if !ok || !f.PQ || f.Key != "ssh-ed25519" || f.Version != "SSH-2.0-OpenSSH_9.9" {
		t.Errorf("post-quantum server: ok=%v %+v", ok, f)
	}
	f, ok = ScanSSH(context.Background(), fakeSSH(t, "curve25519-sha256,diffie-hellman-group14-sha1"))
	issues := strings.Join(f.Issues, ";")
	if !ok || f.PQ || !strings.Contains(issues, "quantum-vulnerable") || !strings.Contains(issues, "SHA-1") {
		t.Errorf("classical server: ok=%v %+v", ok, f)
	}
}
