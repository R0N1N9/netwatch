package hostwatch

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUSBDevices(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "1-1", "idVendor"), "046d")
	write(t, filepath.Join(root, "1-1", "idProduct"), "c31c")
	write(t, filepath.Join(root, "1-1", "manufacturer"), "Logitech")
	write(t, filepath.Join(root, "1-1", "product"), "USB Keyboard")
	write(t, filepath.Join(root, "1-1", "1-1:1.0", "bInterfaceClass"), "03")
	write(t, filepath.Join(root, "1-1", "1-1:1.0", "bInterfaceProtocol"), "01")
	write(t, filepath.Join(root, "1-2", "idVendor"), "0781")
	write(t, filepath.Join(root, "1-2", "idProduct"), "5581")
	write(t, filepath.Join(root, "1-2", "1-2:1.0", "bInterfaceClass"), "08")
	write(t, filepath.Join(root, "1-2:1.0", "bInterfaceClass"), "08") // an interface entry, not a device

	got := USBDevices(root)
	if len(got) != 2 {
		t.Fatalf("found %d devices, want 2: %+v", len(got), got)
	}
	if !got[0].Keyboard || got[0].Label() != "Logitech USB Keyboard [046d:c31c]" {
		t.Errorf("keyboard = %+v (%s)", got[0], got[0].Label())
	}
	if got[1].Keyboard || got[1].Label() != "unnamed device [0781:5581]" {
		t.Errorf("storage = %+v (%s)", got[1], got[1].Label())
	}
}

func TestTooFast(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var human, injected []time.Time
	for i := 0; i < 60; i++ {
		human = append(human, start.Add(time.Duration(i)*110*time.Millisecond))     // ~9 keys/s
		injected = append(injected, start.Add(time.Duration(i)*8*time.Millisecond)) // ~125 keys/s
	}
	if TooFast(human) {
		t.Error("fast human typing flagged")
	}
	if !TooFast(injected) {
		t.Error("injected keystrokes not flagged")
	}
}

func TestSurfaceDiffAndBaseline(t *testing.T) {
	day1 := Surface{Taken: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC),
		Listeners: []string{"tcp 0.0.0.0:22"}, Hosts: []string{"aa:aa 192.168.1.20 pi-hole"}}
	day2 := Surface{Taken: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC),
		Listeners: []string{"tcp 0.0.0.0:22", "tcp 0.0.0.0:4444"}, Hosts: []string{}, USB: []string{"SanDisk [0781:5581]"}}
	want := []string{
		"+ listener    tcp 0.0.0.0:4444",
		"- host        aa:aa 192.168.1.20 pi-hole",
		"+ usb         SanDisk [0781:5581]",
	}
	if got := Diff(day1, day2); !reflect.DeepEqual(got, want) {
		t.Errorf("diff = %q\nwant  %q", got, want)
	}

	store := &SurfaceStore{Dir: t.TempDir()}
	if _, since, ok := store.Baseline(day1); !ok || since != "NetWatch first ran today" {
		t.Errorf("first run: since=%q ok=%v", since, ok)
	}
	base, since, ok := store.Baseline(day2)
	if !ok || since != "2026-11-18" || len(Diff(base, day2)) != 3 {
		t.Errorf("second day: since=%q ok=%v diff=%v", since, ok, Diff(base, day2))
	}
}
