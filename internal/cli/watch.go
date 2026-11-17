package cli

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/r0n1n9/netwatch/internal/netif"
	"github.com/r0n1n9/netwatch/internal/tui"
)

var watchAuto time.Duration

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "WATCH — launch the live NetWatch deck (TUI)",
	Long:  "WATCH opens the full-screen NetWatch deck: live device map, ICE alerts and stats.\nNeeds raw-socket access: run with sudo, or grant cap_net_raw (see README).",
	RunE: func(cmd *cobra.Command, args []string) error {
		info, err := netif.Discover()
		if err != nil {
			return err
		}
		m := tui.New(info, watchAuto)
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			if strings.Contains(err.Error(), "operation not permitted") {
				return fmt.Errorf("permission denied opening raw socket.\n  fix: sudo setcap cap_net_raw,cap_net_admin+eip $(which netrunner)   (or run with sudo)")
			}
			return err
		}
		return nil
	},
}

func init() {
	watchCmd.Flags().DurationVar(&watchAuto, "auto", 20*time.Second, "auto-sweep interval (0 to disable)")
	rootCmd.AddCommand(watchCmd)
}
