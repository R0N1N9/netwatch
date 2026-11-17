// Package theme defines NetWatch's cyberpunk visual language (shared by the
// CLI output and the TUI deck).
package theme

import "github.com/charmbracelet/lipgloss"

// Palette — neon-on-black.
var (
	Cyan    = lipgloss.Color("#00f0ff")
	Magenta = lipgloss.Color("#ff2bd6")
	Green   = lipgloss.Color("#39ff14")
	Amber   = lipgloss.Color("#ffb000")
	Red     = lipgloss.Color("#ff2e4d")
	Dim     = lipgloss.Color("#5a6a7a")
	Fg      = lipgloss.Color("#d7e4ec")
	Bg      = lipgloss.Color("#0a0e14")
)

// Reusable styles.
var (
	Title = lipgloss.NewStyle().Bold(true).Foreground(Bg).Background(Cyan).Padding(0, 1)
	Brand = lipgloss.NewStyle().Bold(true).Foreground(Magenta)
	H     = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	Label = lipgloss.NewStyle().Foreground(Dim)
	Value = lipgloss.NewStyle().Foreground(Fg)
	Good  = lipgloss.NewStyle().Foreground(Green)
	Warn  = lipgloss.NewStyle().Foreground(Amber)
	Crit  = lipgloss.NewStyle().Bold(true).Foreground(Red)
	Faint = lipgloss.NewStyle().Foreground(Dim)

	Box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Cyan).
		Padding(0, 1)
)

// Banner is the NetWatch wordmark.
func Banner() string {
	art := "" +
		"███╗   ██╗███████╗████████╗██╗    ██╗ █████╗ ████████╗ ██████╗██╗  ██╗\n" +
		"████╗  ██║██╔════╝╚══██╔══╝██║    ██║██╔══██╗╚══██╔══╝██╔════╝██║  ██║\n" +
		"██╔██╗ ██║█████╗     ██║   ██║ █╗ ██║███████║   ██║   ██║     ███████║\n" +
		"██║╚██╗██║██╔══╝     ██║   ██║███╗██║██╔══██║   ██║   ██║     ██╔══██║\n" +
		"██║ ╚████║███████╗   ██║   ╚███╔███╔╝██║  ██║   ██║   ╚██████╗██║  ██║\n" +
		"╚═╝  ╚═══╝╚══════╝   ╚═╝    ╚══╝╚══╝ ╚═╝  ╚═╝   ╚═╝    ╚═════╝╚═╝  ╚═╝"
	tag := Faint.Render("        // defensive network watch console · guard the wall //")
	return Brand.Render(art) + "\n" + tag
}
