// Package tui renders the NetWatch deck: a live, cyberpunk network console.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/r0n1n9/netwatch/internal/ice"
	"github.com/r0n1n9/netwatch/internal/model"
	"github.com/r0n1n9/netwatch/internal/netif"
	"github.com/r0n1n9/netwatch/internal/sweep"
	"github.com/r0n1n9/netwatch/internal/theme"
)

type tickMsg time.Time
type sweepDoneMsg struct {
	devices []model.Device
	err     error
}

// Model is the NetWatch deck state.
type Model struct {
	info    *netif.Info
	engine  *ice.Engine
	devices []model.Device
	alerts  []model.Alert
	trusted map[string]bool

	selected   int
	sweeping   bool
	lastSweep  time.Time
	autoEvery  time.Duration
	spin       spinner.Model
	width      int
	height     int
	now        time.Time
	statusLine string
}

// New builds the deck model for an interface.
func New(info *netif.Info, autoEvery time.Duration) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Cyan)
	return Model{
		info:       info,
		engine:     ice.New(),
		trusted:    map[string]bool{},
		autoEvery:  autoEvery,
		spin:       sp,
		now:        time.Now(),
		statusLine: "deck online",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, tick(), m.runSweep())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) runSweep() tea.Cmd {
	info := m.info
	return func() tea.Msg {
		opt := sweep.Default()
		opt.Duration = 2500 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		devices, err := sweep.Run(ctx, info, opt)
		return sweepDoneMsg{devices: devices, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "s":
			if !m.sweeping {
				m.sweeping = true
				m.statusLine = "sweeping segment…"
				return m, m.runSweep()
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.devices)-1 {
				m.selected++
			}
		case "t":
			if m.selected >= 0 && m.selected < len(m.devices) {
				ip := m.devices[m.selected].IP.String()
				m.trusted[ip] = !m.trusted[ip]
				m.devices[m.selected].Trusted = m.trusted[ip]
				state := "TRUSTED"
				if !m.trusted[ip] {
					state = "untrusted"
				}
				m.statusLine = fmt.Sprintf("%s marked %s", ip, state)
			}
		}
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		var cmds []tea.Cmd
		cmds = append(cmds, tick())
		if m.autoEvery > 0 && !m.sweeping && time.Since(m.lastSweep) >= m.autoEvery {
			m.sweeping = true
			m.statusLine = "auto-sweep…"
			cmds = append(cmds, m.runSweep())
		}
		return m, tea.Batch(cmds...)

	case sweepDoneMsg:
		m.sweeping = false
		m.lastSweep = time.Now()
		if msg.err != nil {
			m.statusLine = theme.Crit.Render("sweep error: " + msg.err.Error())
			return m, nil
		}
		m.devices = msg.devices
		newAlerts := m.engine.Observe(msg.devices, m.trusted)
		for _, a := range newAlerts {
			m.alerts = append([]model.Alert{a}, m.alerts...)
		}
		if len(m.alerts) > 100 {
			m.alerts = m.alerts[:100]
		}
		if m.selected >= len(m.devices) {
			m.selected = max(0, len(m.devices)-1)
		}
		m.statusLine = fmt.Sprintf("watch active · %d hosts · %d alerts", len(m.devices), len(m.alerts))
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) View() string {
	if m.width < 60 || m.height < 18 {
		return theme.Warn.Render("terminal too small — resize to at least 60×18")
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 1

	leftW := m.width * 46 / 100
	rightW := m.width - leftW - 1

	devices := panel("DEVICES", m.renderDevices(leftW-4, bodyH-2), leftW, bodyH, theme.Cyan)

	alertH := bodyH*62/100 - 1
	statsH := bodyH - alertH - 1
	alerts := panel("ICE · ALERTS", m.renderAlerts(rightW-4, alertH-2), rightW, alertH, threatAccent(m.alerts))
	stats := panel("STATS", m.renderStats(), rightW, statsH, theme.Magenta)
	right := lipgloss.JoinVertical(lipgloss.Left, alerts, stats)

	body := lipgloss.JoinHorizontal(lipgloss.Top, devices, " ", right)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) renderHeader() string {
	clock := m.now.Format("15:04:05")
	title := theme.Title.Render(" NETWATCH ▸ BLACKWALL ")
	right := theme.Faint.Render(clock)
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	bar := title + strings.Repeat(" ", gap) + right

	status := "● WATCH ACTIVE"
	statusStyled := theme.Good.Render(status)
	if m.sweeping {
		statusStyled = m.spin.View() + lipgloss.NewStyle().Foreground(theme.Cyan).Render(" scanning")
	}
	meta := fmt.Sprintf("%s %s · %s %s · %s %s · %s",
		theme.Label.Render("iface"), theme.Value.Render(m.info.Iface.Name),
		theme.Label.Render("net"), theme.Value.Render(m.info.IPNet.String()),
		theme.Label.Render("gw"), theme.Value.Render(gwStr(m.info)),
		statusStyled,
	)
	return bar + "\n" + meta
}

func (m Model) renderDevices(w, h int) string {
	if len(m.devices) == 0 {
		return theme.Faint.Render("no hosts yet — press [s] to sweep")
	}
	var b strings.Builder
	start := 0
	if m.selected >= h {
		start = m.selected - h + 1
	}
	end := min(len(m.devices), start+h)
	for i := start; i < end; i++ {
		d := m.devices[i]
		marker := "  "
		if i == m.selected {
			marker = lipgloss.NewStyle().Foreground(theme.Magenta).Render("▸ ")
		}
		tag := ""
		switch {
		case d.IsSelf:
			tag = theme.Good.Render(" you")
		case d.IsGateway:
			tag = theme.Warn.Render(" gw")
		}
		if d.Trusted {
			tag += theme.Good.Render(" ✓")
		}
		name := d.Vendor
		if d.Hostname != "" {
			name = d.Hostname
		}
		if name == "" {
			name = "unknown"
		}
		line := fmt.Sprintf("%-15s %s%s", d.IP.String(), clip(name, w-20), tag)
		if i == m.selected {
			line = lipgloss.NewStyle().Foreground(theme.Cyan).Bold(true).Render(line)
		} else {
			line = theme.Value.Render(line)
		}
		b.WriteString(marker + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderAlerts(w, h int) string {
	if len(m.alerts) == 0 {
		return theme.Good.Render("✓ no anomalies · the wall holds")
	}
	var b strings.Builder
	end := min(len(m.alerts), h)
	for i := 0; i < end; i++ {
		a := m.alerts[i]
		b.WriteString(sevTag(a.Severity) + " " + theme.Value.Render(clip(a.Message, w-10)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderStats() string {
	var newCount, threats int
	for _, a := range m.alerts {
		if a.Kind == "new-device" {
			newCount++
		}
		if a.Severity == model.SevCritical {
			threats++
		}
	}
	threatStyle := theme.Good
	if threats > 0 {
		threatStyle = theme.Crit
	}
	last := "never"
	if !m.lastSweep.IsZero() {
		last = time.Since(m.lastSweep).Truncate(time.Second).String() + " ago"
	}
	rows := []string{
		theme.Label.Render("hosts   ") + theme.H.Render(fmt.Sprintf("%d", len(m.devices))),
		theme.Label.Render("new     ") + theme.Warn.Render(fmt.Sprintf("%d", newCount)),
		theme.Label.Render("threats ") + threatStyle.Render(fmt.Sprintf("%d", threats)),
		theme.Label.Render("sweep   ") + theme.Faint.Render(last),
		theme.Faint.Render(m.statusLine),
	}
	return strings.Join(rows, "\n")
}

func (m Model) renderFooter() string {
	keys := []struct{ k, d string }{
		{"s", "sweep"}, {"↑↓", "select"}, {"t", "trust"}, {"q", "quit"},
	}
	var parts []string
	for _, kv := range keys {
		parts = append(parts, theme.Brand.Render("["+kv.k+"] ")+theme.Faint.Render(kv.d))
	}
	return strings.Join(parts, "  ")
}

// --- rendering helpers ---

func panel(title, body string, w, h int, accent lipgloss.Color) string {
	inner := h - 2 // borders
	head := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(title)
	content := head + "\n" + body
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Width(w-2).
		Height(inner).
		Padding(0, 1).
		Render(content)
}

func sevTag(s model.Severity) string {
	switch s {
	case model.SevCritical:
		return theme.Crit.Render("CRIT")
	case model.SevWarn:
		return theme.Warn.Render("WARN")
	default:
		return theme.Faint.Render("INFO")
	}
}

func threatAccent(alerts []model.Alert) lipgloss.Color {
	for _, a := range alerts {
		if a.Severity == model.SevCritical {
			return theme.Red
		}
	}
	return theme.Cyan
}

func gwStr(info *netif.Info) string {
	if info.Gateway == nil {
		return "unknown"
	}
	return info.Gateway.String()
}

func clip(s string, n int) string {
	if n < 1 {
		n = 1
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
