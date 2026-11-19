package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/r0n1n9/netwatch/internal/netif"
	"github.com/r0n1n9/netwatch/internal/server"
	"github.com/r0n1n9/netwatch/internal/theme"
)

var (
	guiAddr   string
	guiAuto   time.Duration
	guiDemo   bool
	guiNoOpen bool
	guiTab    bool
	guiSonar  string
	guiComp   string
	guiData   string
)

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "GUI — launch the NetWatch web console",
	Long:  "GUI starts the NetWatch console (embedded web UI) on localhost and streams live\nnetwork state to it. Use --demo to preview with sample data (no privileges needed).",
	RunE: func(cmd *cobra.Command, args []string) error {
		var info *netif.Info
		if guiDemo {
			info = syntheticInfo()
		} else {
			var err error
			info, err = netif.Discover()
			if err != nil {
				return err
			}
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		srv := server.New(info, guiAuto, guiDemo)
		srv.SonarAddr = guiSonar
		srv.CompanionAddr = guiComp
		srv.DataDir = guiData
		url := "http://" + guiAddr
		// Check the address is free before anything else, so a second copy says so plainly
		// instead of opening the browser on the first one and then failing.
		if probe, err := net.Listen("tcp", guiAddr); err != nil {
			return fmt.Errorf("cannot use %s: %v\n\nNetWatch is probably already running. Stop the old one first:\n    pkill -x netwatch\n(add sudo if it was started with sudo), or run this one on another port:\n    netwatch gui --addr 127.0.0.1:7980", guiAddr, err)
		} else {
			probe.Close()
		}

		fmt.Println(theme.Banner())
		fmt.Println()
		fmt.Printf("  %s %s\n", theme.H.Render("console:"), theme.Good.Render(url))
		mode := "live watch"
		if guiDemo {
			mode = "demo (sample data)"
		}
		fmt.Printf("  %s %s · iface %s · %s\n\n", theme.Label.Render("mode:"), theme.Value.Render(mode), theme.Value.Render(info.Iface.Name), theme.Faint.Render("ctrl-c to stop"))

		fmt.Printf("  %s %s\n", theme.Label.Render("copilot:"), theme.Value.Render(srv.CopilotName()))
		if guiSonar != "" {
			fmt.Printf("  %s %s\n", theme.Label.Render("sonar:"), theme.Value.Render("listening for CSI frames on udp "+guiSonar))
		}
		fmt.Println()

		if !guiNoOpen {
			profile := ""
			if guiData != "" && !guiDemo {
				profile = filepath.Join(guiData, "window")
			}
			how := openConsole(url, profile, guiTab)
			fmt.Printf("  %s %s\n\n", theme.Label.Render("window:"), theme.Value.Render(how))
		}

		if err := srv.Run(ctx, guiAddr); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	guiCmd.Flags().StringVar(&guiAddr, "addr", "127.0.0.1:7979", "listen address for the console")
	guiCmd.Flags().DurationVar(&guiAuto, "auto", 20*time.Second, "auto-sweep interval (0 to disable)")
	guiCmd.Flags().BoolVar(&guiDemo, "demo", false, "serve sample data instead of live capture (no privileges needed)")
	guiCmd.Flags().BoolVar(&guiNoOpen, "no-open", false, "do not open a window or the browser")
	guiCmd.Flags().BoolVar(&guiTab, "browser", false, "open the console as a tab in your default browser instead of a window of its own")
	guiCmd.Flags().StringVar(&guiSonar, "sonar-udp", "", "ingest ESP32 CSI frames on this UDP address, e.g. 0.0.0.0:5566 (off by default)")
	guiCmd.Flags().StringVar(&guiComp, "companion", "", "serve the read-only phone companion over HTTPS on this address, e.g. 0.0.0.0:7443 (off by default)")
	guiCmd.Flags().StringVar(&guiData, "data-dir", defaultDataDir(), "where clips, the room, logs, the ledger and module settings are stored (empty = memory only)")
	rootCmd.AddCommand(guiCmd)
}

// appBrowsers can show a page in a window of its own, with no tabs and no
// address bar. Best first: Chrome and Edge also have the speech service the
// VOICE button needs, which Chromium and Brave are built without.
var appBrowsers = []struct{ bin, name string }{
	{"google-chrome-stable", "Google Chrome"}, {"google-chrome", "Google Chrome"},
	{"microsoft-edge-stable", "Microsoft Edge"}, {"microsoft-edge", "Microsoft Edge"},
	{"chromium", "Chromium"}, {"chromium-browser", "Chromium"},
	{"brave-browser", "Brave"}, {"vivaldi-stable", "Vivaldi"},
}

// findAppBrowser returns the first such browser that look can find. want, if
// set (NETWATCH_BROWSER), names the one to use.
func findAppBrowser(want string, look func(string) (string, error)) (path, name string) {
	if want != "" {
		if p, err := look(want); err == nil {
			return p, filepath.Base(want)
		}
		return "", ""
	}
	for _, b := range appBrowsers {
		if p, err := look(b.bin); err == nil {
			return p, b.name
		}
	}
	return "", ""
}

// windowArgs asks the browser for an application window on url. A profile
// directory of its own keeps the window separate from everyday browsing: its
// own entry in the task bar, its own remembered size and microphone permission.
func windowArgs(url, profile string) []string {
	args := []string{"--app=" + url, "--class=NetWatch", "--window-size=1500,940", "--no-first-run", "--no-default-browser-check"}
	if profile != "" {
		args = append(args, "--user-data-dir="+profile)
	}
	return args
}

// openConsole shows the console: in a window of its own when a browser that
// can do that is installed, otherwise as a tab in the default browser. It
// returns a line saying which.
func openConsole(url, profile string, tab bool) string {
	time.Sleep(400 * time.Millisecond)
	if !tab && os.Geteuid() != 0 { // these browsers refuse to start as root
		if path, name := findAppBrowser(os.Getenv("NETWATCH_BROWSER"), exec.LookPath); path != "" {
			cmd := exec.Command(path, windowArgs(url, profile)...)
			if err := cmd.Start(); err == nil {
				go cmd.Wait()
				return "its own window (" + name + ") · --browser opens a normal tab instead"
			}
		}
	}
	_ = exec.Command("xdg-open", url).Start()
	switch {
	case tab:
		return "a tab in your default browser"
	case os.Geteuid() == 0:
		return "a tab in your default browser (running as root, so no window of its own)"
	}
	return "a tab in your default browser — install Google Chrome, Edge or Chromium and NetWatch opens in a window of its own"
}

func syntheticInfo() *netif.Info {
	_, ipnet, _ := net.ParseCIDR("192.168.1.0/24")
	return &netif.Info{
		Iface:   &net.Interface{Index: 0, Name: "wlan0"},
		IP:      net.ParseIP("192.168.1.50").To4(),
		IPNet:   ipnet,
		Gateway: net.ParseIP("192.168.1.1").To4(),
	}
}

// defaultDataDir follows the XDG convention: $XDG_DATA_HOME/netwatch or ~/.local/share/netwatch.
func defaultDataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d + "/netwatch"
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h + "/.local/share/netwatch"
	}
	return ""
}
