package passive

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"

	"github.com/r0n1n9/netwatch/internal/anomaly"
	"github.com/r0n1n9/netwatch/internal/capture"
	"github.com/r0n1n9/netwatch/internal/model"
)

// Enrichment is the passive intelligence accumulated for one host.
type Enrichment struct {
	OS       string `json:"os"`
	OSConf   int    `json:"osConf"`
	JA3      string `json:"ja3"`
	SNI      string `json:"sni"`
	Anomaly  float64 `json:"anomaly"`
	AnomTop  string `json:"anomTop"`
	Beacon   string `json:"beacon"`
	Updated  time.Time `json:"-"`
}

// External is a public-facing flow (for the connection globe).
type External struct {
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	DPort uint16 `json:"dport"`
	Bytes uint64 `json:"bytes"`
	SNI   string `json:"sni"`
	JA3   string `json:"ja3"`
}

// Monitor runs passive capture + analysis on an interface.
type Monitor struct {
	iface  *net.Interface
	selfIP net.IP
	emit   func(model.Alert)

	ft   *FlowTable
	anom *anomaly.Model

	mu       sync.Mutex
	enr      map[string]*Enrichment
	throttle map[string]time.Time

	pktCtr  uint64
	ppsLast uint64
}

// NewMonitor builds a monitor for iface.
func NewMonitor(iface *net.Interface, selfIP net.IP, emit func(model.Alert)) *Monitor {
	return &Monitor{
		iface: iface, selfIP: selfIP, emit: emit,
		ft: NewFlowTable(), anom: anomaly.New(),
		enr: map[string]*Enrichment{}, throttle: map[string]time.Time{},
	}
}

// Run captures until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) error {
	h, err := capture.Open(m.iface)
	if err != nil {
		return err
	}
	defer h.Close()
	_ = h.SetReadTimeout(500 * time.Millisecond)
	go m.periodic(ctx)
	go m.ppsLoop(ctx)
	for {
		if ctx.Err() != nil {
			return nil
		}
		data, err := h.Recv()
		if err != nil {
			continue
		}
		m.handle(data, time.Now())
	}
}

func (m *Monitor) handle(data []byte, t time.Time) {
	pkt := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Lazy)
	ip4, _ := pkt.Layer(layers.LayerTypeIPv4).(*layers.IPv4)
	if ip4 == nil {
		return
	}
	atomic.AddUint64(&m.pktCtr, 1)
	src, dst := ip4.SrcIP.String(), ip4.DstIP.String()
	proto := uint8(ip4.Protocol)

	if tcp, ok := pkt.Layer(layers.LayerTypeTCP).(*layers.TCP); ok {
		m.ft.Observe(FlowKey{src, dst, uint16(tcp.DstPort), proto}, len(data), t)
		if tcp.SYN && !tcp.ACK {
			g := FingerprintSYN(ip4.TTL, tcp.Window, mssFromOptions(tcp.Options))
			m.setOS(src, g)
		}
		if IsClientHello(tcp.Payload) {
			if _, hash, sni, ok := JA3(tcp.Payload); ok {
				m.setTLS(src, hash, sni)
			}
		}
		return
	}
	if udp, ok := pkt.Layer(layers.LayerTypeUDP).(*layers.UDP); ok {
		m.ft.Observe(FlowKey{src, dst, uint16(udp.DstPort), proto}, len(data), t)
		if udp.SrcPort == 53 || udp.DstPort == 53 {
			if dns, ok := pkt.Layer(layers.LayerTypeDNS).(*layers.DNS); ok {
				for _, q := range dns.Questions {
					m.inspectDNS(src, string(q.Name), q.Type.String())
				}
			}
		}
	}
}

func (m *Monitor) periodic(ctx context.Context) {
	t := time.NewTicker(12 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		m.ft.Prune(5*time.Minute, now)

		for ip, f := range m.ft.DeviceFeatures(now) {
			if ip == m.selfIP.String() {
				continue
			}
			score, top := m.anom.Score(ip, f)
			m.anom.Observe(ip, f)
			m.setAnomaly(ip, score, top)
			if score >= 72 && m.allow("anom:"+ip, 2*time.Minute) {
				m.emit(model.Alert{
					Time: now, Severity: model.SevWarn, Kind: "anomaly", Source: ip,
					Message: fmt.Sprintf("behavioral anomaly on %s (score %.0f, driven by %s)", ip, score, top),
				})
			}
		}
		for _, fl := range m.ft.Snapshot() {
			if !IsPublic(net.ParseIP(fl.Key.Dst)) {
				continue
			}
			if bv := AnalyzeBeacon(fl.Contacts); bv.Beacon {
				m.setBeacon(fl.Key.Src, fmt.Sprintf("%s:%d", fl.Key.Dst, fl.Key.DPort), bv.MeanSecs)
				if m.allow("beacon:"+fl.Key.Src+fl.Key.Dst, 5*time.Minute) {
					m.emit(model.Alert{
						Time: now, Severity: model.SevWarn, Kind: "beacon", Source: fl.Key.Src,
						Message: fmt.Sprintf("periodic beacon %s → %s:%d every ~%.0fs (possible C2)", fl.Key.Src, fl.Key.Dst, fl.Key.DPort, bv.MeanSecs),
					})
				}
			}
		}
	}
}

func (m *Monitor) ppsLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			atomic.StoreUint64(&m.ppsLast, atomic.SwapUint64(&m.pktCtr, 0))
		}
	}
}

func (m *Monitor) inspectDNS(src, name, qtype string) {
	v := ScoreDNS(name, qtype)
	if !v.Tunnel && v.DGA < 0.7 {
		return
	}
	if !m.allow("dns:"+name, 3*time.Minute) {
		return
	}
	sev := model.SevWarn
	if v.Tunnel {
		sev = model.SevCritical
	}
	msg := v.Reason
	if msg == "" {
		msg = fmt.Sprintf("suspicious DNS query %q (DGA %.2f)", v.Name, v.DGA)
	} else {
		msg = fmt.Sprintf("%s: %q (entropy %.1f)", v.Reason, v.Name, v.Entropy)
	}
	m.emit(model.Alert{Time: time.Now(), Severity: sev, Kind: "dns", Source: src, Message: msg})
}

// ---- enrichment accessors ----

func (m *Monitor) ent(ip string) *Enrichment {
	e := m.enr[ip]
	if e == nil {
		e = &Enrichment{}
		m.enr[ip] = e
	}
	return e
}
func (m *Monitor) setOS(ip string, g OSGuess) {
	m.mu.Lock(); defer m.mu.Unlock()
	e := m.ent(ip); e.OS, e.OSConf, e.Updated = g.OS, g.Confidence, time.Now()
}
func (m *Monitor) setTLS(ip, ja3, sni string) {
	m.mu.Lock(); defer m.mu.Unlock()
	e := m.ent(ip); e.JA3, e.SNI, e.Updated = ja3, sni, time.Now()
}
func (m *Monitor) setAnomaly(ip string, score float64, top string) {
	m.mu.Lock(); defer m.mu.Unlock()
	e := m.ent(ip); e.Anomaly, e.AnomTop = score, top
}
func (m *Monitor) setBeacon(ip, dst string, secs float64) {
	m.mu.Lock(); defer m.mu.Unlock()
	e := m.ent(ip); e.Beacon = fmt.Sprintf("%s (~%.0fs)", dst, secs)
}

// Enrichment returns a copy of the intelligence for ip.
func (m *Monitor) Enrichment(ip string) Enrichment {
	m.mu.Lock(); defer m.mu.Unlock()
	if e := m.enr[ip]; e != nil {
		return *e
	}
	return Enrichment{}
}

// Externals returns public-facing flows for the globe.
func (m *Monitor) Externals() []External {
	var out []External
	for _, f := range m.ft.Snapshot() {
		if !IsPublic(net.ParseIP(f.Key.Dst)) {
			continue
		}
		e := m.Enrichment(f.Key.Src)
		out = append(out, External{Src: f.Key.Src, Dst: f.Key.Dst, DPort: f.Key.DPort, Bytes: f.Bytes, SNI: e.SNI, JA3: e.JA3})
	}
	return out
}

// PPS returns the most recent packets-per-second sample.
func (m *Monitor) PPS() uint64 { return atomic.LoadUint64(&m.ppsLast) }

func (m *Monitor) allow(key string, d time.Duration) bool {
	m.mu.Lock(); defer m.mu.Unlock()
	if last, ok := m.throttle[key]; ok && time.Since(last) < d {
		return false
	}
	m.throttle[key] = time.Now()
	return true
}

func mssFromOptions(opts []layers.TCPOption) uint16 {
	for _, o := range opts {
		if o.OptionType == layers.TCPOptionKindMSS && len(o.OptionData) == 2 {
			return binary.BigEndian.Uint16(o.OptionData)
		}
	}
	return 0
}
