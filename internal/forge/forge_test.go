package forge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigBlob(t *testing.T) {
	b, err := ConfigBlob(Settings{SSID: "ronin net", Pass: `p"a\ss=1`, Host: "192.168.1.50", Port: 5566})
	if err != nil || len(b) != 4096 {
		t.Fatalf("blob: %d bytes, %v", len(b), err)
	}
	text := strings.TrimRight(string(b), "\x00")
	if text != "NWCFG1\nssid=ronin net\npass=p\"a\\ss=1\nhost=192.168.1.50\nport=5566\nname=\n" {
		t.Errorf("blob = %q", text)
	}
	for _, bad := range []Settings{{SSID: ""}, {SSID: strings.Repeat("x", 33)}, {SSID: "a", Pass: strings.Repeat("x", 65)}, {SSID: "a\nhost=evil"}, {SSID: "a", Pass: "x\ny"}} {
		if _, err := ConfigBlob(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestBoardsFromSysfs(t *testing.T) {
	root := t.TempDir()
	old := sysRoot
	sysRoot = root
	defer func() { sysRoot = old }()
	mk := func(tty, vid, pid, product string) {
		dev := filepath.Join(root, "devices", "usb1", "1-"+tty)
		iface := filepath.Join(dev, "1-"+tty+":1.0")
		os.MkdirAll(iface, 0o755)
		os.WriteFile(filepath.Join(dev, "idVendor"), []byte(vid+"\n"), 0o644)
		os.WriteFile(filepath.Join(dev, "idProduct"), []byte(pid+"\n"), 0o644)
		if product != "" {
			os.WriteFile(filepath.Join(dev, "product"), []byte(product+"\n"), 0o644)
		}
		os.MkdirAll(filepath.Join(root, "class", "tty", tty), 0o755)
		os.Symlink(iface, filepath.Join(root, "class", "tty", tty, "device"))
	}
	mk("ttyACM0", "303a", "1001", "USB JTAG/serial debug unit")
	mk("ttyUSB0", "10c4", "ea60", "CP2102N USB to UART Bridge Controller")
	mk("ttyACM1", "046d", "c52b", "Some Mouse Receiver") // not a board
	os.MkdirAll(filepath.Join(root, "class", "tty", "ttyS0"), 0o755)
	got := Boards()
	if len(got) != 2 || got[0].Port != "/dev/ttyACM0" || !got[0].Native || got[1].Port != "/dev/ttyUSB0" || got[1].Native || !strings.Contains(got[1].Chip, "UART port") {
		t.Errorf("boards = %+v", got)
	}
}

func TestFlashHandsEsptoolTheRightFilesAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	tool := filepath.Join(dir, "esptool")
	// A stand-in for esptool: note the arguments and the settings it was given.
	os.WriteFile(tool, []byte("#!/bin/sh\necho \"$@\" > "+record+"\nfor a in \"$@\"; do case \"$a\" in */settings.bin) head -c 60 \"$a\" >> "+record+";; esac; done\nprintf 'Writing at 0x00010000... (50 %%)\\rWriting at 0x00020000... (100 %%)\\nHard resetting via RTS pin...\\n'\n"), 0o755)
	cfg, _ := ConfigBlob(Settings{SSID: "home", Pass: "secret-pass", Host: "10.0.0.5", Port: 5566})
	var lines []string
	if err := Flash(context.Background(), []string{tool}, "/dev/ttyACM0", []byte("firmware-image"), cfg, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	rec, _ := os.ReadFile(record)
	args := strings.SplitN(string(rec), "\n", 2)
	if !strings.HasPrefix(args[0], "--chip esp32s3 -p /dev/ttyACM0 -b 460800 write_flash 0x0 ") || !strings.Contains(args[0], "image.bin 0x190000 ") || !strings.Contains(args[1], "ssid=home\npass=secret-pass") {
		t.Errorf("esptool was run with %q and saw settings %q", args[0], args[1])
	}
	if len(lines) != 3 || !strings.Contains(lines[1], "100 %") {
		t.Errorf("progress lines = %q", lines)
	}
	tmp := strings.Fields(args[0])[8] // the image path: its directory must be gone
	if _, err := os.Stat(filepath.Dir(tmp)); !os.IsNotExist(err) {
		t.Errorf("the temporary directory with the Wi-Fi password was left behind: %s", filepath.Dir(tmp))
	}
	// settings only: no image argument
	Flash(context.Background(), []string{tool}, "/dev/ttyACM0", nil, cfg, func(string) {})
	rec, _ = os.ReadFile(record)
	if strings.Contains(string(rec), "image.bin") || !strings.Contains(string(rec), "write_flash 0x190000 ") {
		t.Errorf("settings-only flash ran %q", strings.SplitN(string(rec), "\n", 2)[0])
	}
	os.WriteFile(tool, []byte("#!/bin/sh\necho 'A fatal error occurred: Failed to connect to ESP32-S3: No serial data received.'\nexit 2\n"), 0o755)
	if err := Flash(context.Background(), []string{tool}, "/dev/ttyACM0", nil, cfg, func(l string) { lines = append(lines, l) }); err == nil || !strings.Contains(lines[len(lines)-1], "Failed to connect") {
		t.Errorf("a failed flash returned %v, last line %q", err, lines[len(lines)-1])
	}
	if _, _, ok := Image("node", ""); ok {
		t.Log("a node image is embedded in this build")
	}
	if _, _, ok := Image("toaster", ""); ok {
		t.Error("found an image for a board kind that does not exist")
	}
}

// The embedded image and this package must agree on where the settings go.
func TestEmbeddedImageHasTheSettingsPartitionWhereWeWriteIt(t *testing.T) {
	for kind := range Kinds {
		img, _, ok := Image(kind, "")
		if !ok {
			t.Skipf("no %s image is embedded in this build", kind)
		}
		if img[0] != 0xE9 || img[0x10000] != 0xE9 {
			t.Errorf("%s: not a merged ESP image (bootloader %#x, app %#x)", kind, img[0], img[0x10000])
		}
		found := ""
		for i := 0x8000; i+32 <= 0x9000 && img[i] == 0xAA && img[i+1] == 0x50; i += 32 {
			name := strings.TrimRight(string(img[i+12:i+28]), "\x00")
			off := uint32(img[i+4]) | uint32(img[i+5])<<8 | uint32(img[i+6])<<16 | uint32(img[i+7])<<24
			if name == "nwcfg" {
				found = "0x" + strings.ToLower(strings.TrimLeft(strings.ToUpper(hex32(off)), "0"))
			}
		}
		if found != ConfigOffset {
			t.Errorf("%s: the image's nwcfg partition is at %q, NetWatch writes settings at %s", kind, found, ConfigOffset)
		}
	}
}

func hex32(v uint32) string {
	const d = "0123456789abcdef"
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = d[v&15]
		v >>= 4
	}
	return string(b)
}

func TestStageReadsBothEsptoolDialects(t *testing.T) {
	cases := []struct {
		line, stage string
		pct         int
		ok          bool
	}{
		{"Serial port /dev/ttyACM0", "connect", 0, true},
		{"Connecting....", "connect", 0, true},
		{"Chip is ESP32-S3 (QFN56) (revision v0.2)", "", 0, false},
		{"Writing at 0x00010000... (25 %)", "write", 25, true},
		{"Writing at 0x000f0000... (100 %)", "write", 100, true},
		{"Writing at 0x0001f8e5 [==>                           ]  10.5% 16384/155632 bytes...", "write", 10, true},
		{"Writing at 0x000cc000 [==============================] 100.0% 836512/836512 bytes...", "write", 100, true},
		{"Wrote 836512 bytes (512345 compressed) at 0x00000000 in 12.3 seconds", "verify", 100, true},
		{"Hash of data verified.", "verify", 100, true},
		{"Hard resetting via RTS pin...", "restart", 100, true},
		{"A fatal error occurred: Failed to connect to ESP32-S3", "", 0, false},
	}
	for _, c := range cases {
		stage, pct, ok := Stage(c.line)
		if stage != c.stage || pct != c.pct || ok != c.ok {
			t.Errorf("Stage(%q) = %q %d %v, want %q %d %v", c.line, stage, pct, ok, c.stage, c.pct, c.ok)
		}
	}
}

// pipx installs esptool into ~/.local/bin, which is often not on PATH.
func TestToolIsFoundOffThePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // an empty PATH: nothing to find there
	t.Setenv("SUDO_USER", "")
	if tool, _ := Tool(); tool != nil && strings.HasPrefix(tool[0], home) {
		t.Fatalf("found a tool before one was installed: %v", tool)
	}
	bin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "esptool"), []byte("#!/bin/sh\n"), 0o755)
	tool, name := Tool()
	if len(tool) != 1 || tool[0] != filepath.Join(bin, "esptool") || !strings.Contains(name, ".local/bin") {
		t.Errorf("Tool() = %v, %q", tool, name)
	}
}

func TestToolPathGivenByTheOperator(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SUDO_USER", "")
	good := filepath.Join(dir, "esptool")
	os.WriteFile(good, []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "rm"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "esptool.py"), []byte("not executable"), 0o644)
	for _, bad := range []string{filepath.Join(dir, "rm"), filepath.Join(dir, "esptool.py"), "esptool", filepath.Join(dir, "missing", "esptool")} {
		if CheckTool(bad) == nil {
			t.Errorf("accepted %q as esptool", bad)
		}
	}
	old := ToolPath
	defer func() { ToolPath = old }()
	ToolPath = good
	if tool, name := Tool(); len(tool) != 1 || tool[0] != good || !strings.Contains(name, dir) {
		t.Errorf("Tool() with a given path = %v, %q", tool, name)
	}
	if len(Searched()) < 4 || Searched()[0] != "your PATH" {
		t.Errorf("Searched() = %v", Searched())
	}
}

func TestFlashCanBeCancelledMidway(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "esptool")
	os.WriteFile(tool, []byte("#!/bin/sh\necho 'Connecting....'\necho 'Writing at 0x00010000... (12 %)'\nexec sleep 30\n"), 0o755)
	cfg, _ := ConfigBlob(Settings{SSID: "home", Port: 5566})
	ctx, cancel := context.WithCancel(context.Background())
	var lines []string
	go func() {
		time.Sleep(400 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := Flash(ctx, []string{tool}, "/dev/ttyACM0", []byte("image"), cfg, func(l string) { lines = append(lines, l) })
	if err != ErrCancelled || time.Since(start) > 5*time.Second {
		t.Fatalf("cancelled flash returned %v after %v", err, time.Since(start))
	}
	if len(lines) != 2 {
		t.Errorf("lines before the cancel = %q", lines)
	}
	left, _ := filepath.Glob(filepath.Join(os.TempDir(), "netwatch-forge-*"))
	if len(left) != 0 {
		t.Errorf("a cancelled flash left the settings file behind: %v", left)
	}
}
