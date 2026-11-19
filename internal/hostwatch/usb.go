// Package hostwatch watches the machine NetWatch runs on: USB devices being
// plugged in, how the attack surface changes from day to day, and the screen lock.
package hostwatch

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// USBDev is one attached USB device.
type USBDev struct {
	Path     string `json:"path"` // sysfs name, e.g. "1-2"
	VID      string `json:"vid"`
	PID      string `json:"pid"`
	Vendor   string `json:"vendor"`
	Product  string `json:"product"`
	Keyboard bool   `json:"keyboard"` // presents a HID keyboard interface
}

// Label is a short human description.
func (d USBDev) Label() string {
	name := strings.TrimSpace(d.Vendor + " " + d.Product)
	if name == "" {
		name = "unnamed device"
	}
	return name + " [" + d.VID + ":" + d.PID + "]"
}

// SysUSB is where Linux lists USB devices.
const SysUSB = "/sys/bus/usb/devices"

// USBDevices lists the devices under a sysfs root.
func USBDevices(root string) []USBDev {
	var out []USBDev
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		vid := read(filepath.Join(dir, "idVendor"))
		if vid == "" {
			continue // an interface, not a device
		}
		d := USBDev{Path: e.Name(), VID: vid, PID: read(filepath.Join(dir, "idProduct")),
			Vendor: read(filepath.Join(dir, "manufacturer")), Product: read(filepath.Join(dir, "product"))}
		ifaces, _ := filepath.Glob(filepath.Join(dir, e.Name()+":*"))
		for _, i := range ifaces {
			if read(filepath.Join(i, "bInterfaceClass")) == "03" && read(filepath.Join(i, "bInterfaceProtocol")) == "01" {
				d.Keyboard = true
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// WatchUSB polls for newly attached devices. The first scan is the baseline.
// onNew is called for each new device; onFastKeys if a new keyboard then types
// faster than a person can (only checked when /dev/input is readable).
func WatchUSB(ctx context.Context, root string, onNew func(USBDev), onFastKeys func(USBDev)) {
	known := map[string]bool{}
	for _, d := range USBDevices(root) {
		known[d.Path+d.VID+d.PID] = true
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := map[string]bool{}
		for _, d := range USBDevices(root) {
			key := d.Path + d.VID + d.PID
			now[key] = true
			if known[key] {
				continue
			}
			onNew(d)
			if d.Keyboard && onFastKeys != nil {
				go func(d USBDev) {
					if watchKeys(ctx, filepath.Join(root, d.Path), 20*time.Second) {
						onFastKeys(d)
					}
				}(d)
			}
		}
		known = now
	}
}

// TooFast reports whether key presses arrive faster than a person types:
// 25 or more within any one-second span.
func TooFast(presses []time.Time) bool {
	const burst = 25
	for i := 0; i+burst-1 < len(presses); i++ {
		if presses[i+burst-1].Sub(presses[i]) <= time.Second {
			return true
		}
	}
	return false
}

// watchKeys reads the new keyboard's input events for a while and reports
// whether its typing rate is inhuman. Returns false if the events are unreadable.
func watchKeys(ctx context.Context, devDir string, d time.Duration) bool {
	time.Sleep(700 * time.Millisecond) // let the input node appear
	var nodes []string
	for _, pat := range []string{"*/*/input/input*/event*", "*/input/input*/event*"} {
		m, _ := filepath.Glob(filepath.Join(devDir, pat))
		nodes = append(nodes, m...)
	}
	if len(nodes) == 0 {
		return false
	}
	f, err := os.Open(filepath.Join("/dev/input", filepath.Base(nodes[0])))
	if err != nil {
		return false
	}
	defer f.Close()
	stop := time.AfterFunc(d, func() { f.Close() })
	defer stop.Stop()
	go func() { <-ctx.Done(); f.Close() }()

	var presses []time.Time
	buf := make([]byte, 24) // struct input_event on 64-bit Linux
	for {
		if _, err := f.Read(buf); err != nil {
			return false
		}
		typ, val := binary.LittleEndian.Uint16(buf[16:]), int32(binary.LittleEndian.Uint32(buf[20:]))
		if typ == 1 && val == 1 { // EV_KEY, key down
			presses = append(presses, time.Now())
			if len(presses) > 400 {
				presses = presses[len(presses)-400:]
			}
			if TooFast(presses) {
				return true
			}
		}
	}
}
