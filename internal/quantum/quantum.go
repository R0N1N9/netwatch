// Package quantum inventories the cryptography of TLS and SSH services on the
// local network: protocol version, cipher, key type and size, certificate
// expiry, and whether the key exchange would survive a quantum computer.
//
// It only talks to private addresses, completes an ordinary handshake, and
// sends no application data.
package quantum

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Finding describes one service.
type Finding struct {
	Addr     string   `json:"addr"`
	Proto    string   `json:"proto"` // tls | ssh
	Version  string   `json:"version"`
	Cipher   string   `json:"cipher,omitempty"`
	Key      string   `json:"key,omitempty"` // e.g. "RSA 2048", "ECDSA P-256"
	KEX      string   `json:"kex,omitempty"`
	PQ       bool     `json:"pq"` // offers a post-quantum (hybrid) key exchange
	DaysLeft int      `json:"daysLeft,omitempty"`
	Issues   []string `json:"issues"`
}

// SSH key exchanges that include a post-quantum component.
var sshPQ = []string{"mlkem768x25519-sha256", "sntrup761x25519-sha512", "sntrup761x25519-sha512@openssh.com", "mlkem1024nistp384-sha384", "mlkem768nistp256-sha256"}

const dialTimeout = 1500 * time.Millisecond

// allowed reports whether NetWatch may probe this address.
func allowed(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

// Scan inventories the given hosts on the common TLS and SSH ports.
func Scan(ctx context.Context, hosts []string) []Finding {
	type job struct {
		addr string
		ssh  bool
	}
	var jobs []job
	for _, h := range hosts {
		if !allowed(h) {
			continue
		}
		for _, p := range []string{"443", "8443", "993", "8883"} {
			jobs = append(jobs, job{net.JoinHostPort(h, p), false})
		}
		jobs = append(jobs, job{net.JoinHostPort(h, "22"), true})
	}
	var (
		mu  sync.Mutex
		out []Finding
		wg  sync.WaitGroup
		sem = make(chan struct{}, 24)
	)
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			var f Finding
			var ok bool
			if j.ssh {
				f, ok = ScanSSH(ctx, j.addr)
			} else {
				f, ok = ScanTLS(ctx, j.addr)
			}
			if ok {
				mu.Lock()
				out = append(out, f)
				mu.Unlock()
			}
		}(j)
	}
	wg.Wait()
	return out
}

func handshake(ctx context.Context, addr string, cfg *tls.Config) (*tls.ConnectionState, error) {
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: dialTimeout}, Config: cfg}
	c, cancel := context.WithTimeout(ctx, 2*dialTimeout)
	defer cancel()
	conn, err := d.DialContext(c, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	st := conn.(*tls.Conn).ConnectionState()
	return &st, nil
}

// ScanTLS completes a TLS handshake and reports what was negotiated. ok is
// false when nothing speaks TLS at addr.
func ScanTLS(ctx context.Context, addr string) (Finding, bool) {
	host, _, _ := net.SplitHostPort(addr)
	if !allowed(host) {
		return Finding{}, false
	}
	// Certificates are being inventoried, not trusted, so verification is off.
	st, err := handshake(ctx, addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10})
	if err != nil {
		return Finding{}, false
	}
	f := Finding{Addr: addr, Proto: "tls", Version: tls.VersionName(st.Version), Cipher: tls.CipherSuiteName(st.CipherSuite), Issues: []string{}}
	if st.Version < tls.VersionTLS12 {
		f.Issues = append(f.Issues, "obsolete protocol version")
	}
	if len(st.PeerCertificates) > 0 {
		cert := st.PeerCertificates[0]
		switch k := cert.PublicKey.(type) {
		case *rsa.PublicKey:
			f.Key = fmt.Sprintf("RSA %d", k.N.BitLen())
			if k.N.BitLen() < 2048 {
				f.Issues = append(f.Issues, "RSA key under 2048 bits")
			}
		case *ecdsa.PublicKey:
			f.Key = "ECDSA " + k.Curve.Params().Name
		case ed25519.PublicKey:
			f.Key = "Ed25519"
		default:
			f.Key = cert.PublicKeyAlgorithm.String()
		}
		f.DaysLeft = int(time.Until(cert.NotAfter).Hours() / 24)
		switch {
		case f.DaysLeft < 0:
			f.Issues = append(f.Issues, "certificate has expired")
		case f.DaysLeft < 30:
			f.Issues = append(f.Issues, fmt.Sprintf("certificate expires in %d days", f.DaysLeft))
		}
	}
	// A second handshake offering only the hybrid post-quantum group tells us
	// whether the server can do one.
	if st.Version >= tls.VersionTLS13 {
		if _, err := handshake(ctx, addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519MLKEM768}}); err == nil {
			f.PQ, f.KEX = true, "X25519MLKEM768 (hybrid post-quantum)"
		}
	}
	if !f.PQ {
		f.KEX = "classical only"
		f.Issues = append(f.Issues, "key exchange is quantum-vulnerable: traffic recorded today could be decrypted later")
	}
	return f, true
}

// ScanSSH reads an SSH server's banner and the algorithms it offers.
func ScanSSH(ctx context.Context, addr string) (Finding, bool) {
	host, _, _ := net.SplitHostPort(addr)
	if !allowed(host) {
		return Finding{}, false
	}
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Finding{}, false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * dialTimeout))
	r := bufio.NewReader(conn)
	banner, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(banner, "SSH-") {
		return Finding{}, false
	}
	f := Finding{Addr: addr, Proto: "ssh", Version: strings.TrimSpace(banner), Issues: []string{}}
	if strings.HasPrefix(banner, "SSH-1.") {
		f.Issues = append(f.Issues, "obsolete protocol version")
	}
	fmt.Fprint(conn, "SSH-2.0-NetWatch_inventory\r\n")
	kex, hostKeys, err := readKexInit(r)
	if err != nil {
		f.Issues = append(f.Issues, "could not read the algorithm list")
		return f, true
	}
	for _, k := range kex {
		for _, pq := range sshPQ {
			if k == pq {
				f.PQ, f.KEX = true, k+" (hybrid post-quantum)"
			}
		}
	}
	if !f.PQ {
		if len(kex) > 0 {
			f.KEX = kex[0] + " (classical only)"
		}
		f.Issues = append(f.Issues, "key exchange is quantum-vulnerable: sessions recorded today could be decrypted later")
	}
	if len(hostKeys) > 0 {
		f.Key = hostKeys[0]
	}
	for _, k := range kex {
		if strings.Contains(k, "sha1") {
			f.Issues = append(f.Issues, "still offers SHA-1 key exchange ("+k+")")
			break
		}
	}
	return f, true
}

// readKexInit parses the server's SSH_MSG_KEXINIT, which is sent in the clear.
func readKexInit(r io.Reader) (kex, hostKeys []string, err error) {
	var hdr [5]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return nil, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:4])
	if n < 20 || n > 65536 {
		return nil, nil, fmt.Errorf("bad packet length %d", n)
	}
	body := make([]byte, n-1)
	if _, err = io.ReadFull(r, body); err != nil {
		return nil, nil, err
	}
	payload := body[:len(body)-int(hdr[4])]
	if len(payload) < 17 || payload[0] != 20 {
		return nil, nil, fmt.Errorf("not a KEXINIT")
	}
	p := payload[17:] // message type + 16-byte cookie
	list := func() []string {
		if len(p) < 4 {
			return nil
		}
		l := int(binary.BigEndian.Uint32(p))
		if l > len(p)-4 {
			return nil
		}
		s := string(p[4 : 4+l])
		p = p[4+l:]
		if s == "" {
			return nil
		}
		return strings.Split(s, ",")
	}
	return list(), list(), nil
}
