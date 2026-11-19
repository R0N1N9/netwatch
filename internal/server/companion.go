package server

// The phone companion: a second, read-only listener for a phone on your own
// network. It serves one page (haptic alerts and the X-RAY view) over HTTPS
// with a self-signed certificate, and every data request needs the pairing
// code shown in the REFLEX deck. The console itself stays on localhost.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/r0n1n9/netwatch/internal/webui"
)

// companion is guarded by Server.mu.
type companion struct {
	on          bool
	code        string
	fingerprint string
	port        string
	bad         int
	badSince    time.Time
}

func (s *Server) companionView() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{"on": s.comp.on, "flag": "--companion 0.0.0.0:7443"}
	if s.comp.on {
		out["code"], out["fingerprint"] = s.comp.code, s.comp.fingerprint
		out["url"] = "https://" + net.JoinHostPort(s.companionIP().String(), s.comp.port) + "/?code=" + s.comp.code
	}
	return out
}

// companionIP is the address a phone should use: the one the listener was
// bound to if that is specific, otherwise this machine's address on the LAN.
func (s *Server) companionIP() net.IP {
	if host, _, err := net.SplitHostPort(s.CompanionAddr); err == nil {
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
			return ip
		}
	}
	if s.demo { // the demo's interface is made up; look for a real one
		if addrs, err := net.InterfaceAddrs(); err == nil {
			for _, a := range addrs {
				if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
					return n.IP
				}
			}
		}
	}
	return s.info.IP
}

// selfSigned makes a one-year certificate for this machine's addresses.
func selfSigned(ips []net.IP) (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "NetWatch companion"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: ips, DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	sum := sha256.Sum256(der)
	fp := strings.ToUpper(hex.EncodeToString(sum[:]))
	var pretty []string
	for i := 0; i < 16; i += 2 { // the first eight bytes are enough to compare by eye
		pretty = append(pretty, fp[i:i+2])
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, strings.Join(pretty, ":"), nil
}

func pairingCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// paired checks the pairing code and slows down guessing: after ten wrong
// codes in a minute, everything is refused until the minute is over.
func (s *Server) paired(r *http.Request) (ok bool, status int) {
	got := r.URL.Query().Get("code")
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.comp.badSince) > time.Minute {
		s.comp.bad, s.comp.badSince = 0, now
	}
	if s.comp.bad >= 10 {
		return false, http.StatusTooManyRequests
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.comp.code)) != 1 {
		s.comp.bad++
		return false, http.StatusForbidden
	}
	return true, http.StatusOK
}

// companionHandler is everything the phone can reach.
func (s *Server) companionHandler() http.Handler {
	assets := webui.FS()
	mux := http.NewServeMux()
	file := func(name, ctype string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := fs.ReadFile(assets, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "no-store")
			w.Write(b)
		}
	}
	page := file("companion.html", "text/html; charset=utf-8")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page(w, r)
	})
	mux.HandleFunc("/xray.js", file("xray.js", "application/javascript"))
	mux.HandleFunc("/sonar", func(w http.ResponseWriter, r *http.Request) {
		if ok, code := s.paired(r); !ok {
			http.Error(w, "pairing code required", code)
			return
		}
		writeJSON(w, s.sonarView())
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		if ok, code := s.paired(r); !ok {
			http.Error(w, "pairing code required", code)
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch := make(chan []byte, 8)
		s.clientsMu.Lock()
		s.clients[ch] = struct{}{}
		s.clientsMu.Unlock()
		defer func() {
			s.clientsMu.Lock()
			delete(s.clients, ch)
			s.clientsMu.Unlock()
		}()
		send := func(snap []byte) { // the phone gets the alerts and nothing else
			var v struct {
				Alerts []wireAlert `json:"alerts"`
				Meta   struct {
					Armed bool `json:"sonarArmed"`
				} `json:"meta"`
			}
			if json.Unmarshal(snap, &v) != nil {
				return
			}
			if len(v.Alerts) > 20 {
				v.Alerts = v.Alerts[:20]
			}
			b, _ := json.Marshal(map[string]any{"alerts": v.Alerts, "armed": v.Meta.Armed})
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
		send(s.snapshotJSON())
		keep := time.NewTicker(20 * time.Second)
		defer keep.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-ch:
				send(b)
			case <-keep.C:
				fmt.Fprint(w, ": keepalive\n\n")
				fl.Flush()
			}
		}
	})
	return mux
}

func (s *Server) runCompanion(ctx context.Context) {
	_, port, err := net.SplitHostPort(s.CompanionAddr)
	if err != nil {
		log.Printf("companion: bad address %q: %v", s.CompanionAddr, err)
		return
	}
	cert, fp, err := selfSigned([]net.IP{s.companionIP(), s.info.IP, net.IPv4(127, 0, 0, 1)})
	if err != nil {
		log.Printf("companion: cannot make a certificate: %v", err)
		return
	}
	ln, err := tls.Listen("tcp", s.CompanionAddr, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		log.Printf("companion: cannot listen on %s: %v", s.CompanionAddr, err)
		return
	}
	s.mu.Lock()
	s.comp = companion{on: true, code: pairingCode(), fingerprint: fp, port: port}
	s.mu.Unlock()
	srv := &http.Server{Handler: s.companionHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	log.Printf("companion: listening on %s (pairing code in the REFLEX deck)", s.CompanionAddr)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Printf("companion: %v", err)
	}
}
