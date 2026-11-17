package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/netif"
	"github.com/r0n1n9/netwatch/internal/sweep"
	"github.com/r0n1n9/netwatch/internal/theme"
)

var (
	sweepDuration time.Duration
	sweepNoDNS    bool
	sweepMax      int
)

var sweepCmd = &cobra.Command{
	Use:   "sweep",
	Short: "SWEEP — discover every device on your local subnet (ARP)",
	Long:  "SWEEP injects ARP requests across your subnet and lists the hosts that answer.\nNeeds raw-socket access: run with sudo, or grant the binary cap_net_raw (see README).",
	RunE: func(cmd *cobra.Command, args []string) error {
		info, err := netif.Discover()
		if err != nil {
			return err
		}

		fmt.Println(theme.Title.Render(" NETWATCH · SWEEP "))
		fmt.Printf("%s %s   %s %s   %s %s\n\n",
			theme.Label.Render("iface"), theme.Value.Render(info.Iface.Name),
			theme.Label.Render("subnet"), theme.Value.Render(info.IPNet.String()),
			theme.Label.Render("gateway"), theme.Value.Render(gwStr(info)),
		)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		opt := sweep.Default()
		opt.Duration = sweepDuration
		opt.ResolveDNS = !sweepNoDNS
		opt.MaxHosts = sweepMax

		fmt.Println(theme.Faint.Render(fmt.Sprintf("… sweeping up to %d hosts, listening %s", opt.MaxHosts, opt.Duration)))
		devices, err := sweep.Run(ctx, info, opt)
		if err != nil {
			if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
				return fmt.Errorf("permission denied opening raw socket.\n  fix: sudo setcap cap_net_raw,cap_net_admin+eip $(which netrunner)   (or run with sudo)")
			}
			return err
		}
		renderDevices(devices)
		return nil
	},
}

func init() {
	sweepCmd.Flags().DurationVarP(&sweepDuration, "duration", "d", 3*time.Second, "how long to listen for replies")
	sweepCmd.Flags().BoolVar(&sweepNoDNS, "no-dns", false, "skip reverse-DNS resolution")
	sweepCmd.Flags().IntVar(&sweepMax, "max", 1024, "maximum host addresses to probe")
}

func gwStr(info *netif.Info) string {
	if info.Gateway == nil {
		return "unknown"
	}
	return info.Gateway.String()
}

func renderDevices(devices []model.Device) {
	fmt.Println()
	fmt.Println(theme.H.Render(fmt.Sprintf("◉ %d host(s) online", len(devices))))
	fmt.Println()

	hdr := fmt.Sprintf("  %-16s %-18s %-20s %s", "IP", "MAC", "VENDOR", "HOST / ROLE")
	fmt.Println(theme.Label.Render(hdr))
	fmt.Println(theme.Faint.Render("  " + strings.Repeat("─", 72)))

	for _, d := range devices {
		role := d.Hostname
		tag := ""
		switch {
		case d.IsSelf:
			tag = theme.Good.Render(" [you]")
		case d.IsGateway:
			tag = theme.Warn.Render(" [gateway]")
		}
		vendor := d.Vendor
		if vendor == "" {
			vendor = "unknown"
		}
		line := fmt.Sprintf("  %-16s %-18s %-20s %s%s",
			d.IP.String(), macStr(d.MAC), truncate(vendor, 20), role, tag)
		switch {
		case d.IsSelf:
			fmt.Println(theme.Good.Render(line))
		case d.IsGateway:
			fmt.Println(theme.Value.Render(line))
		default:
			fmt.Println(theme.Value.Render(line))
		}
	}
	fmt.Println()
}

func macStr(m net.HardwareAddr) string {
	if len(m) == 0 {
		return "--:--:--:--:--:--"
	}
	return m.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
