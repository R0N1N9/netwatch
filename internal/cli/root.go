// Package cli wires up the netwatch command tree.
//
// NetWatch is the umbrella console. Its first deck is Watchtower (defensive).
// Offensive (recon/lab) and RUView (3D spatial) decks hang off the same shell.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/r0n1n9/netwatch/internal/theme"
)

var rootCmd = &cobra.Command{
	Use:           "netwatch",
	Short:         "NetWatch — a cyberpunk network security console",
	Long:          theme.Banner(),
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, theme.Crit.Render("!! "+err.Error()))
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(sweepCmd)
}
