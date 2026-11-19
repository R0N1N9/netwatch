package server

// The phone companion: a second listener for a phone on your own network. It
// serves one page (haptic alerts, FIND, X-RAY and clip playback) over HTTPS
// with a self-signed certificate, and every data request needs the pairing
// code shown in the MOBILE deck. The phone can change nothing on the console;
// the one thing it can send is its own view (see dance.go), and only while its
// owner has switched that on. The console itself stays on localhost.

import (
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
	qrcode "github.com/skip2/go-qrcode"
)

// companion is guarded by Server.mu.
type companion struct {
	on          bool
	code        string
	fingerprint string
	port        string
	bad         int
	badSince    time.Time
	srv         *http.Server
	linked      int           // phones with the alert stream open right now
	kick        chan struct{} // closed to drop every open phone stream (a new pairing code)
}

func (s *Server) companionView() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{"on": s.comp.on, "flag": "--companion 0.0.0.0:7443", "linked": s.comp.linked}
	if s.comp.on {
		out["addr"] = net.JoinHostPort(s.companionIP().String(), s.comp.port)
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

// finderView is what the phone needs to point at a device: every host the
// sensor nodes have placed in the room, which node hears each one best, and —
// if the nodes can hear the phone itself — where the phone is.
func (s *Server) finderView(remote string) map[string]any {
	v := s.sonarView()
	hosts, _ := v["hosts"].([]sonarHost)
	ip, _, err := net.SplitHostPort(remote)
	if err != nil {
		ip = remote
	}
	now := time.Now()
	type found struct {
		sonarHost
		Nearest     string `json:"nearest"`     // the node that hears it loudest
		NearestRSSI int    `json:"nearestRssi"` // ...and how loud
		Me          bool   `json:"me"`          // this is the phone asking
	}
	out := []found{}
	me := map[string]any{"ip": ip, "located": false}
	for _, h := range hosts {
		f := found{sonarHost: h, Me: h.IP == ip}
		if rd, ok := s.mod.spec.Hunt(h.MAC, now); ok && len(rd) > 0 {
			f.Nearest, f.NearestRSSI = rd[0].Node, rd[0].RSSI
		}
		if f.Me {
			me = map[string]any{"ip": ip, "located": true, "x": h.X, "y": h.Y, "nodes": h.Nodes, "name": h.Name}
		}
		out = append(out, f)
	}
	return map[string]any{"demo": s.demo, "hosts": out, "me": me, "layout": v["layout"], "walls": v["walls"], "nodes": v["nodes"], "est": v["est"]}
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
	mux.HandleFunc("/companion.css", file("companion.css", "text/css; charset=utf-8"))
	for _, weight := range []string{"500", "600", "700"} { // the console's typeface
		name := "rajdhani-latin-" + weight + "-normal.woff2"
		mux.HandleFunc("/fonts/"+name, file("vendor/fonts/"+name, "font/woff2"))
	}
	mux.HandleFunc("/favicon.svg", file("favicon.svg", "image/svg+xml"))
	mux.HandleFunc("/apple-touch-icon.png", file("apple-touch-icon.png", "image/png"))
	mux.HandleFunc("/finder", func(w http.ResponseWriter, r *http.Request) {
		if ok, code := s.paired(r); !ok {
			http.Error(w, "pairing code required", code)
			return
		}
		writeJSON(w, s.finderView(r.RemoteAddr))
	})
	mux.HandleFunc("/dance.js", file("dance.js", "application/javascript"))
	mux.HandleFunc("/dance", func(w http.ResponseWriter, r *http.Request) { // the clip library
		if ok, code := s.paired(r); !ok {
			http.Error(w, "pairing code required", code)
			return
		}
		writeJSON(w, s.danceList())
	})
	mux.HandleFunc("/dance/clip", func(w http.ResponseWriter, r *http.Request) { // one clip, ready to draw
		if ok, code := s.paired(r); !ok {
			http.Error(w, "pairing code required", code)
			return
		}
		v, found := s.danceScene(r.URL.Query().Get("id"))
		if !found {
			http.Error(w, "no such clip", http.StatusNotFound)
			return
		}
		writeJSON(w, v)
	})
	mux.HandleFunc("/view", s.handlePhoneView) // the one thing a phone can send: where it is looking
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
		s.mu.Lock()
		s.comp.linked++
		kick := s.comp.kick
		s.mu.Unlock()
		defer func() {
			s.clientsMu.Lock()
			delete(s.clients, ch)
			s.clientsMu.Unlock()
			s.mu.Lock()
			if s.comp.linked > 0 {
				s.comp.linked--
			}
			s.mu.Unlock()
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
			case <-kick:
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

const companionDefault = "0.0.0.0:7443"

// startCompanion opens the phone listener on addr. It can be called at start-up
// (the --companion flag, or a remembered choice) or from the MOBILE deck.
func (s *Server) startCompanion(addr string) error {
	s.mu.Lock()
	if s.comp.on {
		s.mu.Unlock()
		return nil
	}
	ctx := s.fg.ctx
	s.mu.Unlock()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad address %q: %v", addr, err)
	}
	s.CompanionAddr = addr
	cert, fp, err := selfSigned([]net.IP{s.companionIP(), s.info.IP, net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return fmt.Errorf("cannot make a certificate: %v", err)
	}
	ln, err := tls.Listen("tcp", addr, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %v", addr, err)
	}
	srv := &http.Server{Handler: s.companionHandler(), ReadHeaderTimeout: 5 * time.Second}
	s.mu.Lock()
	s.comp = companion{on: true, code: pairingCode(), fingerprint: fp, port: port, srv: srv, kick: make(chan struct{})}
	s.mu.Unlock()
	if ctx != nil {
		go func() {
			<-ctx.Done()
			srv.Close()
		}()
	}
	go func() {
		log.Printf("companion: listening on %s (QR code and pairing code in the MOBILE deck)", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("companion: %v", err)
		}
	}()
	return nil
}

// stopCompanion closes the phone listener and drops every linked phone.
func (s *Server) stopCompanion() {
	s.mu.Lock()
	srv := s.comp.srv
	s.comp = companion{}
	s.mu.Unlock()
	if srv != nil {
		srv.Close()
	}
}

// handleMobile is the MOBILE deck: the phone link's state, switching it on and off, a new pairing code.
func (s *Server) handleMobile(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			On      *bool `json:"on"`
			NewCode bool  `json:"newCode"`
		}
		if !decode(w, r, &req) {
			return
		}
		switch {
		case req.On != nil && *req.On:
			if s.demo && s.CompanionAddr == "" {
				writeJSON(w, map[string]any{"ok": false, "error": "demo mode does not open a port on your network from here — start it with: netwatch gui --demo --companion " + companionDefault})
				return
			}
			addr := s.CompanionAddr
			if addr == "" {
				addr = companionDefault
			}
			if err := s.startCompanion(addr); err != nil {
				writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			s.logAction("mobile", "switched the phone link on ("+addr+")")
		case req.On != nil:
			s.stopCompanion()
			s.logAction("mobile", "switched the phone link off")
		case req.NewCode:
			s.mu.Lock()
			if s.comp.on {
				s.comp.code, s.comp.bad = pairingCode(), 0
				if s.comp.kick != nil {
					close(s.comp.kick) // phones linked with the old code lose their stream now
				}
				s.comp.kick = make(chan struct{})
			}
			s.mu.Unlock()
			s.logAction("mobile", "issued a new pairing code; phones linked with the old one were cut off")
		}
		s.saveModules()
	}
	v := s.companionView()
	v["ok"], v["demo"] = true, s.demo
	writeJSON(w, v)
}

// handleMobileQR draws the phone link as a QR code. It is served by the console only.
func (s *Server) handleMobileQR(w http.ResponseWriter, r *http.Request) {
	url, _ := s.companionView()["url"].(string)
	if url == "" {
		http.Error(w, "the phone link is off", http.StatusNotFound)
		return
	}
	png, err := qrcode.Encode(url, qrcode.Medium, 640)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}
