// Package forge flashes ESP32-S3 boards from NetWatch: it finds boards on USB,
// writes one of the embedded firmware images, and writes the board's settings
// (Wi-Fi name and password, where NetWatch is, the node's name) into a small
// partition the firmware reads at start-up. The flashing itself is done by
// Espressif's esptool, which has to be installed.
package forge

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed firmware
var images embed.FS

// ErrCancelled is returned by Flash when the operator stopped it.
var ErrCancelled = errors.New("cancelled")

// ConfigOffset is where the firmware's "nwcfg" partition sits in flash.
const ConfigOffset = "0x190000"

// Kinds of board NetWatch can make, and the image each needs.
var Kinds = map[string]string{"node": "sonar-node.bin", "light": "threat-light.bin"}

// Board is a serial port that looks like an ESP32 development board.
type Board struct {
	Port   string `json:"port"`   // /dev/ttyACM0
	Chip   string `json:"chip"`   // what the USB side says it is
	Native bool   `json:"native"` // the ESP32-S3's own USB port rather than the UART bridge
	VIDPID string `json:"vidpid"`
}

// USB-to-serial chips found on ESP32 boards, and Espressif's own USB.
var known = map[string]string{
	"303a": "Espressif USB (the board's native USB port)",
	"10c4": "Silicon Labs CP210x (UART port)",
	"1a86": "WCH CH34x / CH9102 (UART port)",
	"0403": "FTDI (UART port)",
}

// sysRoot is /sys, replaceable in tests.
var sysRoot = "/sys"

// Boards lists the serial ports that belong to a USB device NetWatch recognises.
func Boards() []Board {
	out := []Board{}
	entries, _ := os.ReadDir(filepath.Join(sysRoot, "class", "tty"))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "ttyACM") && !strings.HasPrefix(name, "ttyUSB") {
			continue
		}
		dir, err := filepath.EvalSymlinks(filepath.Join(sysRoot, "class", "tty", name, "device"))
		if err != nil {
			continue
		}
		for i := 0; i < 4; i++ { // walk up from the interface to the USB device
			vid, err := os.ReadFile(filepath.Join(dir, "idVendor"))
			if err != nil {
				dir = filepath.Dir(dir)
				continue
			}
			v := strings.TrimSpace(string(vid))
			pid, _ := os.ReadFile(filepath.Join(dir, "idProduct"))
			if chip, ok := known[v]; ok {
				if product, err := os.ReadFile(filepath.Join(dir, "product")); err == nil && strings.TrimSpace(string(product)) != "" {
					chip = strings.TrimSpace(string(product)) + " — " + chip
				}
				out = append(out, Board{Port: "/dev/" + name, Chip: chip, Native: v == "303a", VIDPID: v + ":" + strings.TrimSpace(string(pid))})
			}
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// ToolPath, if set, is the esptool to use (from the console's setting or the
// NETWATCH_ESPTOOL environment variable) instead of searching.
var ToolPath = os.Getenv("NETWATCH_ESPTOOL")

// toolDirs lists the places esptool is commonly installed that may not be on
// PATH: pipx and pip put it in ~/.local/bin, which many shells do not include.
func toolDirs() []string {
	var homes []string
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		homes = append(homes, h)
	}
	if u := os.Getenv("SUDO_USER"); u != "" { // started with sudo: the tool belongs to the person, not to root
		homes = append(homes, filepath.Join("/home", u))
	}
	if os.Geteuid() == 0 { // running as root some other way (su, a root shell): look in everyone's home
		if all, err := filepath.Glob("/home/*"); err == nil {
			homes = append(homes, all...)
		}
	}
	var dirs []string
	if d := os.Getenv("PIPX_BIN_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if d := os.Getenv("PIPX_HOME"); d != "" {
		dirs = append(dirs, filepath.Join(d, "venvs", "esptool", "bin"))
	}
	seen := map[string]bool{}
	for _, h := range homes {
		if seen[h] {
			continue
		}
		seen[h] = true
		dirs = append(dirs, filepath.Join(h, ".local", "bin"), filepath.Join(h, ".local", "share", "pipx", "venvs", "esptool", "bin"),
			filepath.Join(h, ".local", "pipx", "venvs", "esptool", "bin"), filepath.Join(h, ".espressif", "python_env"))
	}
	return append(dirs, "/usr/local/bin", "/usr/bin", "/opt/pipx/bin", "/opt/homebrew/bin")
}

// Searched lists where Tool looks, for telling the operator when it finds nothing.
func Searched() []string {
	return append([]string{"your PATH"}, toolDirs()...)
}

func runnable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// CheckTool says whether a path the operator gave can be used as esptool.
func CheckTool(p string) error {
	if base := filepath.Base(p); base != "esptool" && base != "esptool.py" {
		return fmt.Errorf("that is not esptool: the file must be named esptool or esptool.py")
	}
	if !filepath.IsAbs(p) || !runnable(p) {
		return fmt.Errorf("%s is not a program NetWatch can run — give the full path, for example the output of: which esptool", p)
	}
	return nil
}

// Tool finds esptool. It returns the command to run and a name to show.
func Tool() ([]string, string) {
	if ToolPath != "" && CheckTool(ToolPath) == nil {
		return []string{ToolPath}, "esptool (" + filepath.Dir(ToolPath) + ")"
	}
	names := []string{"esptool", "esptool.py"}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return []string{p}, name
		}
	}
	for _, dir := range toolDirs() {
		for _, name := range names {
			if p := filepath.Join(dir, name); runnable(p) {
				return []string{p}, name + " (" + dir + ")"
			}
		}
	}
	if py, err := exec.LookPath("python3"); err == nil && exec.Command(py, "-c", "import esptool").Run() == nil {
		return []string{py, "-m", "esptool"}, "python3 -m esptool"
	}
	return nil, ""
}

// Image returns the firmware for a kind of board: a file you put in
// <dataDir>/firmware wins, otherwise the image embedded in this binary.
func Image(kind, dataDir string) (data []byte, source string, ok bool) {
	file, known := Kinds[kind]
	if !known {
		return nil, "", false
	}
	if dataDir != "" {
		if b, err := os.ReadFile(filepath.Join(dataDir, "firmware", file)); err == nil && len(b) > 4096 {
			return b, "your data directory", true
		}
	}
	if b, err := images.ReadFile("firmware/" + file); err == nil && len(b) > 4096 {
		return b, "built into NetWatch", true
	}
	return nil, "", false
}

// Settings are what a board needs to know to start working.
type Settings struct {
	SSID string
	Pass string
	Host string // the machine running NetWatch
	Port int
	Name string // empty = the board names itself after its hardware address
}

// ConfigBlob renders settings the way the firmware reads them: a marker line,
// then key=value lines, padded to one flash sector.
func ConfigBlob(s Settings) ([]byte, error) {
	for what, v := range map[string]string{"network name": s.SSID, "password": s.Pass, "address": s.Host, "node name": s.Name} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return nil, fmt.Errorf("the %s contains a line break", what)
		}
	}
	switch {
	case s.SSID == "" || len(s.SSID) > 32:
		return nil, fmt.Errorf("the Wi-Fi name must be 1 to 32 characters")
	case len(s.Pass) > 64:
		return nil, fmt.Errorf("the Wi-Fi password is at most 64 characters")
	case len(s.Host) > 39 || len(s.Name) > 23:
		return nil, fmt.Errorf("the address or node name is too long")
	}
	text := fmt.Sprintf("NWCFG1\nssid=%s\npass=%s\nhost=%s\nport=%d\nname=%s\n", s.SSID, s.Pass, s.Host, s.Port, s.Name)
	blob := make([]byte, 4096)
	copy(blob, text)
	return blob, nil
}

// Flash writes the settings, and the firmware image too unless image is nil.
// The files handed to esptool live in a private temporary directory that is
// removed afterwards, because the settings hold the Wi-Fi password.
func Flash(ctx context.Context, tool []string, port string, image, config []byte, log func(string)) error {
	if len(tool) == 0 {
		return fmt.Errorf("esptool is not installed")
	}
	dir, err := os.MkdirTemp("", "netwatch-forge-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	args := append([]string{}, tool[1:]...)
	args = append(args, "--chip", "esp32s3", "-p", port, "-b", "460800", "write_flash")
	if image != nil {
		if err := os.WriteFile(filepath.Join(dir, "image.bin"), image, 0o600); err != nil {
			return err
		}
		args = append(args, "0x0", filepath.Join(dir, "image.bin"))
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.bin"), config, 0o600); err != nil {
		return err
	}
	args = append(args, ConfigOffset, filepath.Join(dir, "settings.bin"))
	cmd := exec.CommandContext(ctx, tool[0], args...)
	cmd.WaitDelay = 2 * time.Second // after a cancel, do not wait on a pipe the killed tool left open
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Split(scanLines)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				log(line)
			}
		}
		close(done)
	}()
	err = cmd.Run()
	pw.Close()
	<-done
	if ctx.Err() == context.Canceled {
		return ErrCancelled
	}
	if err != nil {
		return fmt.Errorf("esptool failed: %w", err)
	}
	return nil
}

// scanLines splits on \n and on \r, which esptool uses to redraw its progress line.
func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Stages a flash goes through, in order.
var Stages = []string{"connect", "write", "verify", "restart"}

// Stage reads one line of esptool output and says which stage it belongs to
// and, while writing, how far along it is. ok is false for lines that say
// nothing about progress. It understands both esptool 4 ("... (25 %)") and
// esptool 5 ("... [===>  ]  25.0% 65536/262144 bytes").
func Stage(line string) (stage string, percent int, ok bool) {
	l := strings.ToLower(line)
	switch {
	case strings.HasPrefix(l, "writing at"):
		i := strings.LastIndex(l, "%")
		if i < 0 {
			return "write", 0, true
		}
		j := i
		for j > 0 && (l[j-1] == ' ' || l[j-1] == '.' || (l[j-1] >= '0' && l[j-1] <= '9')) {
			j--
		}
		var whole float64
		fmt.Sscanf(strings.TrimSpace(l[j:i]), "%f", &whole)
		if whole < 0 || whole > 100 {
			whole = 0
		}
		return "write", int(whole), true
	case strings.HasPrefix(l, "connecting"), strings.HasPrefix(l, "serial port"), strings.HasPrefix(l, "connected to"):
		return "connect", 0, true
	case strings.Contains(l, "hash of data verified"), strings.HasPrefix(l, "wrote "), strings.Contains(l, "verifying"):
		return "verify", 100, true
	case strings.Contains(l, "hard resetting"), strings.Contains(l, "leaving..."):
		return "restart", 100, true
	}
	return "", 0, false
}
