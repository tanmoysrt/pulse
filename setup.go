package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	accent = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a78bfa"}
	dim    = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}

	titleStyle  = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(dim)
)

type choice struct {
	label string
	desc  string
}

var (
	exposeChoices = []choice{
		{"Local network", "reachable by other devices on your Wi-Fi / LAN"},
		{"Public tunnel", "a public https link, reachable from anywhere"},
	}
	notifyChoices = []choice{
		{"Off", "no desktop pop-ups on this machine"},
		{"On", "pop up a desktop notification when an agent needs you"},
	}
)

// runWizard interactively fills any startup choice not already fixed by a flag.
// It no-ops on a non-interactive stdin (piped/CI), leaving flag values/defaults.
func runWizard(o opts) opts {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return o
	}
	var steps []string
	if !o.local && !o.tunnelSet {
		steps = append(steps, "expose")
	}
	if !o.noAuth && !o.passwordSet {
		steps = append(steps, "password")
	}
	if !o.notifySet {
		steps = append(steps, "notify")
	}
	if len(steps) == 0 {
		return o
	}

	ti := textinput.New()
	ti.Placeholder = "leave blank to auto-generate"
	ti.CharLimit = 64
	ti.Width = 34

	m := wizModel{steps: steps, o: o, input: ti}
	m.focusStep()
	res, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return o
	}
	fm := res.(wizModel)
	if fm.quit {
		fmt.Println("pulse: setup cancelled")
		os.Exit(0)
	}
	return fm.o
}

type wizModel struct {
	steps  []string
	i      int
	cursor int
	o      opts
	input  textinput.Model
	quit   bool
}

func (m wizModel) Init() tea.Cmd { return textinput.Blink }

func (m *wizModel) focusStep() {
	if m.step() == "password" {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.cursor = 0
}

func (m wizModel) step() string { return m.steps[m.i] }

func (m wizModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	switch key.String() {
	case "ctrl+c", "esc":
		m.quit = true
		return m, tea.Quit
	case "up", "k":
		if m.step() != "password" && m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.step() != "password" && m.cursor < 1 {
			m.cursor++
		}
	case "enter":
		return m.commit()
	}
	if m.step() == "password" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// commit records the current step's answer and advances (or quits on the last).
func (m wizModel) commit() (tea.Model, tea.Cmd) {
	switch m.step() {
	case "expose":
		m.o.tunnel = m.cursor == 1
	case "password":
		m.o.password = strings.TrimSpace(m.input.Value())
	case "notify":
		m.o.localNotify = m.cursor == 1
	}
	if m.i == len(m.steps)-1 {
		return m, tea.Quit
	}
	m.i++
	m.focusStep()
	return m, textinput.Blink
}

func (m wizModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n\n", accentStyle.Render("◆ Pulse"), dimStyle.Render(fmt.Sprintf("setup · %d of %d", m.i+1, len(m.steps))))

	switch m.step() {
	case "expose":
		b.WriteString(titleStyle.Render("How should pulse be reachable?") + "\n\n")
		b.WriteString(m.renderChoices(exposeChoices))
	case "password":
		b.WriteString(titleStyle.Render("Set a login password") + "\n")
		b.WriteString(dimStyle.Render("Used on the login page; scanning the QR skips it.") + "\n\n")
		b.WriteString("  " + m.input.View() + "\n")
	case "notify":
		b.WriteString(titleStyle.Render("Desktop notifications on this machine?") + "\n\n")
		b.WriteString(m.renderChoices(notifyChoices))
	}

	b.WriteString("\n" + dimStyle.Render("↑/↓ move · enter confirm · esc cancel"))
	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func (m wizModel) renderChoices(choices []choice) string {
	var b strings.Builder
	for i, c := range choices {
		if i == m.cursor {
			b.WriteString(accentStyle.Render("  › "+c.label) + "  " + dimStyle.Render(c.desc) + "\n")
		} else {
			b.WriteString(dimStyle.Render("    "+c.label+"  "+c.desc) + "\n")
		}
	}
	return b.String()
}

// renderSummary is the post-setup screen: the URL to open, its QR, and the login
// password. Intentionally spare — only what you need to get pulse on a phone.
func renderSummary(url, scope, password, qr string) string {
	label := map[string]string{"public": "Public link", "LAN": "On your network", "local": "On this machine"}[scope]
	var b strings.Builder
	b.WriteString("\n" + accentStyle.Render("◆ Pulse is running") + "\n\n")
	if qr != "" {
		b.WriteString(indent(qr, 2) + "\n")
	}
	b.WriteString("  " + dimStyle.Render(label) + "\n")
	b.WriteString("  " + accentStyle.Render(url) + "\n")
	if password != "" {
		b.WriteString("\n  " + dimStyle.Render("Password") + "  " + titleStyle.Render(password) + "\n")
	}
	b.WriteString("\n  " + dimStyle.Render("pulse claude — start a session · Ctrl-C — quit") + "\n")
	return b.String()
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
