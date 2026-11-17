// Package oui resolves a MAC address prefix to a hardware vendor.
//
// It ships with a small built-in table of common vendors. For full coverage,
// an IEEE OUI file (CSV of "AABBCC,Vendor") can be loaded at runtime via Load.
package oui

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

var (
	mu    sync.RWMutex
	table = builtin()
)

// Lookup returns the vendor for a MAC, or "" if unknown.
func Lookup(mac net.HardwareAddr) string {
	if len(mac) < 3 {
		return ""
	}
	key := fmt.Sprintf("%02X%02X%02X", mac[0], mac[1], mac[2])
	mu.RLock()
	v, ok := table[key]
	mu.RUnlock()
	if ok {
		return v
	}
	// Bit 1 of the first octet set => locally administered (randomized MAC).
	if mac[0]&0x02 != 0 {
		return "(randomized MAC)"
	}
	return ""
}

// Load merges an IEEE-style OUI CSV ("AABBCC,Vendor Name") into the table.
func Load(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	mu.Lock()
	defer mu.Unlock()
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		prefix := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(parts[0], ":", ""), "-", ""))
		if len(prefix) < 6 {
			continue
		}
		table[prefix[:6]] = strings.TrimSpace(parts[1])
		n++
	}
	return n, sc.Err()
}

// builtin returns a starter set of widely-seen vendor prefixes.
func builtin() map[string]string {
	return map[string]string{
		"00000C": "Cisco",
		"001A11": "Google",
		"3C5AB4": "Google",
		"F4F5E8": "Google",
		"DCA632": "Raspberry Pi",
		"B827EB": "Raspberry Pi",
		"E45F01": "Raspberry Pi",
		"2CCF67": "Raspberry Pi",
		"001451": "Apple",
		"3C0754": "Apple",
		"A4C361": "Apple",
		"F0D1A9": "Apple",
		"AC87A3": "Apple",
		"D0817A": "Apple",
		"001D0F": "TP-Link",
		"50C7BF": "TP-Link",
		"A42BB0": "TP-Link",
		"001583": "Netgear",
		"A040A0": "Netgear",
		"20E52A": "Netgear",
		"0018E7": "Samsung",
		"5440AD": "Samsung",
		"D022BE": "Samsung",
		"B0A737": "Roku",
		"DC3A5E": "Amazon",
		"68543D": "Amazon",
		"74C246": "Amazon",
		"F0272D": "Ubiquiti",
		"FCECDA": "Ubiquiti",
		"788A20": "Ubiquiti",
		"002637": "Samsung",
		"000569": "VMware",
		"005056": "VMware",
		"080027": "VirtualBox",
		"525400": "QEMU/KVM",
		"00163E": "Xen",
		"001C42": "Parallels",
	}
}
