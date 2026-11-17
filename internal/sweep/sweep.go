// Package sweep implements SWEEP — active ARP discovery of hosts on the local
// subnet. It injects ARP requests for every host address and collects replies.
package sweep

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"

	"github.com/r0n1n9/netwatch/internal/capture"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/netif"
	"github.com/r0n1n9/netwatch/internal/oui"
)

// Options tunes a sweep.
type Options struct {
	Duration    time.Duration // how long to listen for replies
	MaxHosts    int           // cap on addresses probed
	ResolveDNS  bool          // reverse-DNS discovered hosts
	PerHostWait time.Duration // pacing between injected requests
}

// Default returns sensible sweep options.
func Default() Options {
	return Options{
		Duration:    3 * time.Second,
		MaxHosts:    1024,
		ResolveDNS:  true,
		PerHostWait: 2 * time.Millisecond,
	}
}

// Run performs the sweep and returns discovered devices sorted by IP.
func Run(ctx context.Context, info *netif.Info, opt Options) ([]model.Device, error) {
	h, err := capture.OpenProto(info.Iface, capture.ProtoARP)
	if err != nil {
		return nil, err
	}
	defer h.Close()
	_ = h.SetReadTimeout(400 * time.Millisecond)

	var mu sync.Mutex
	found := map[string]*model.Device{}
	note := func(ip net.IP, mac net.HardwareAddr) {
		mu.Lock()
		defer mu.Unlock()
		key := ip.String()
		d, ok := found[key]
		if !ok {
			found[key] = &model.Device{
				IP:        append(net.IP(nil), ip...),
				MAC:       append(net.HardwareAddr(nil), mac...),
				Vendor:    oui.Lookup(mac),
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
				IsSelf:    ip.Equal(info.IP),
				IsGateway: info.Gateway != nil && ip.Equal(info.Gateway),
			}
			return
		}
		d.LastSeen = time.Now()
	}

	// Always account for ourselves.
	note(info.IP, info.Iface.HardwareAddr)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := h.Recv()
			if err != nil {
				continue // read timeout
			}
			pkt := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)
			al := pkt.Layer(layers.LayerTypeARP)
			if al == nil {
				continue
			}
			arp, _ := al.(*layers.ARP)
			if arp == nil || arp.Operation != layers.ARPReply {
				continue
			}
			note(net.IP(arp.SourceProtAddress), net.HardwareAddr(arp.SourceHwAddress))
		}
	}()

	// Inject a request for every candidate host.
	hosts := netif.Hosts(info.IPNet, opt.MaxHosts)
	srcMAC := info.Iface.HardwareAddr
	for _, target := range hosts {
		if ctx.Err() != nil {
			break
		}
		if target.Equal(info.IP) {
			continue
		}
		frame, err := buildARPRequest(srcMAC, info.IP, target)
		if err != nil {
			continue
		}
		_ = h.Send(frame)
		if opt.PerHostWait > 0 {
			time.Sleep(opt.PerHostWait)
		}
	}

	// Listen a while longer for stragglers.
	select {
	case <-time.After(opt.Duration):
	case <-ctx.Done():
	}
	close(stop)
	wg.Wait()

	devices := make([]model.Device, 0, len(found))
	for _, d := range found {
		devices = append(devices, *d)
	}
	if opt.ResolveDNS {
		resolveNames(ctx, devices)
	}
	sort.Slice(devices, func(i, j int) bool {
		return ipLess(devices[i].IP, devices[j].IP)
	})
	return devices, nil
}

func buildARPRequest(srcMAC net.HardwareAddr, srcIP, dstIP net.IP) ([]byte, error) {
	eth := layers.Ethernet{
		SrcMAC:       srcMAC,
		DstMAC:       net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		EthernetType: layers.EthernetTypeARP,
	}
	arp := layers.ARP{
		AddrType:          layers.LinkTypeEthernet,
		Protocol:          layers.EthernetTypeIPv4,
		HwAddressSize:     6,
		ProtAddressSize:   4,
		Operation:         layers.ARPRequest,
		SourceHwAddress:   srcMAC,
		SourceProtAddress: srcIP.To4(),
		DstHwAddress:      net.HardwareAddr{0, 0, 0, 0, 0, 0},
		DstProtAddress:    dstIP.To4(),
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(buf, opts, &eth, &arp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func resolveNames(ctx context.Context, devices []model.Device) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	r := &net.Resolver{}
	for i := range devices {
		wg.Add(1)
		sem <- struct{}{}
		go func(d *model.Device) {
			defer wg.Done()
			defer func() { <-sem }()
			c, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
			defer cancel()
			names, err := r.LookupAddr(c, d.IP.String())
			if err == nil && len(names) > 0 {
				d.Hostname = trimDot(names[0])
			}
		}(&devices[i])
	}
	wg.Wait()
}

func trimDot(s string) string {
	for len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

func ipLess(a, b net.IP) bool {
	a4, b4 := a.To4(), b.To4()
	if a4 == nil || b4 == nil {
		return a.String() < b.String()
	}
	for i := 0; i < 4; i++ {
		if a4[i] != b4[i] {
			return a4[i] < b4[i]
		}
	}
	return false
}
