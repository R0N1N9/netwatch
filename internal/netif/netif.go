// Package netif discovers the active network interface, its IPv4 subnet, and
// the default gateway — all without external dependencies.
package netif

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
)

// Info describes the interface NetWatch will watch.
type Info struct {
	Iface   *net.Interface
	IP      net.IP     // our IPv4 on this interface
	IPNet   *net.IPNet // the subnet
	Gateway net.IP     // default gateway (may be nil)
}

// Discover picks the interface carrying the default route. If none is found it
// falls back to the first up, non-loopback interface with an IPv4 address.
func Discover() (*Info, error) {
	gw, gwIface := defaultRoute()

	if gwIface != "" {
		if iface, err := net.InterfaceByName(gwIface); err == nil {
			if ip, ipnet := firstIPv4(iface); ip != nil {
				return &Info{Iface: iface, IP: ip, IPNet: ipnet, Gateway: gw}, nil
			}
		}
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if ip, ipnet := firstIPv4(&iface); ip != nil {
			ifCopy := iface
			return &Info{Iface: &ifCopy, IP: ip, IPNet: ipnet, Gateway: gw}, nil
		}
	}
	return nil, fmt.Errorf("no active IPv4 interface found")
}

func firstIPv4(iface *net.Interface) (net.IP, *net.IPNet) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		return ip4, &net.IPNet{IP: ip4.Mask(ipnet.Mask), Mask: ipnet.Mask}
	}
	return nil, nil
}

// defaultRoute parses /proc/net/route for the IPv4 default gateway.
func defaultRoute() (net.IP, string) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		// Destination 00000000 with the RTF_GATEWAY flag (0x2) => default route.
		if fields[1] != "00000000" {
			continue
		}
		gw := parseHexIPLE(fields[2])
		if gw != nil {
			return gw, fields[0]
		}
	}
	return nil, ""
}

// parseHexIPLE decodes a little-endian hex IPv4 as found in /proc/net/route.
func parseHexIPLE(h string) net.IP {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 4 {
		return nil
	}
	v := binary.LittleEndian.Uint32(b)
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, v)
	if ip.Equal(net.IPv4zero) {
		return nil
	}
	return ip
}

// Hosts enumerates usable host IPs in a subnet (network and broadcast excluded).
// It caps output at maxHosts to avoid scanning absurdly large ranges.
func Hosts(ipnet *net.IPNet, maxHosts int) []net.IP {
	if ipnet == nil {
		return nil
	}
	base := ipnet.IP.Mask(ipnet.Mask).To4()
	if base == nil {
		return nil
	}
	ones, bits := ipnet.Mask.Size()
	count := 1 << uint(bits-ones)
	if count <= 2 {
		return nil
	}
	start := binary.BigEndian.Uint32(base)
	var out []net.IP
	for i := 1; i < count-1; i++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, start+uint32(i))
		out = append(out, ip)
		if len(out) >= maxHosts {
			break
		}
	}
	return out
}
