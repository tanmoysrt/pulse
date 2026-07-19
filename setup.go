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

// runWizard interactively confirms saved setup or fills choices not fixed by flags.
// Non-interactive runs reuse saved setup and otherwise keep flag values/defaults.
func runWizard(o opts) opts {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return applySavedSetup(o)
	}
	if saved, err := readSetup(); err == nil {
		if hasSetupOverrides(o) {
			return applySetup(o, saved)
		}
		return runSavedWizard(o, saved)
	}
	return runSetupWizard(o)
}

func runSavedWizard(o opts, saved *setupRecord) opts {
	m := wizModel{steps: []string{"saved"}, o: o, saved: saved}
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

func runSetupWizard(o opts) opts {
	steps := setupSteps(o)
	if len(steps) == 0 {
		return o
	}

	ti := textinput.New()
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

func setupSteps(o opts) []string {
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
	return steps
}

func hasSetupOverrides(o opts) bool {
	return o.local || o.noAuth || o.tunnelSet || o.notifySet || o.passwordSet || o.portSet
}

func applySavedSetup(o opts) opts {
	saved, err := readSetup()
	if err != nil {
		return o
	}
	return applySetup(o, saved)
}

func applySetup(o opts, saved *setupRecord) opts {
	if !o.local && !o.tunnelSet {
		o.tunnel = saved.Tunnel
	}
	if !o.notifySet {
		o.localNotify = saved.Notify
	}
	if !o.noAuth && !o.passwordSet {
		o.passwordHash = saved.PasswordHash
	}
	return o
}

type wizModel struct {
	steps  []string
	i      int
	cursor int
	o      opts
	input  textinput.Model
	quit   bool
	saved  *setupRecord
}

func (m wizModel) Init() tea.Cmd { return textinput.Blink }

func (m *wizModel) focusStep() {
	m.input.Reset()
	switch m.step() {
	case "password":
		m.input.Placeholder = "leave blank to auto-generate"
		m.input.EchoMode = textinput.EchoPassword
		m.input.Focus()
	default:
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
	case "saved":
		if m.cursor == 0 {
			m.o = applySetup(m.o, m.saved)
			return m, tea.Quit
		}
		m.steps = setupSteps(m.o)
		m.i = 0
		m.focusStep()
		return m, textinput.Blink
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
	case "saved":
		mode := "Local network"
		if m.saved.Tunnel {
			mode = "Public tunnel"
		}
		b.WriteString(titleStyle.Render("Use saved setup?") + "\n\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("%s · desktop notifications %s", mode, onOff(m.saved.Notify))) + "\n\n")
		b.WriteString(m.renderChoices([]choice{{"Start with saved setup", "use these settings"}, {"Redo setup", "replace the saved settings"}}))
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

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
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

// renderSummary is the post-setup screen: the QR and the URL to open. Kept
// spare on purpose; the password is never printed (scan the QR, or you set it).
func renderSummary(url, scope, qr string) string {
	label := map[string]string{"public": "Public link", "LAN": "On your network", "local": "On this machine"}[scope]
	var b strings.Builder
	b.WriteString("\n" + accentStyle.Render("◆ Pulse is running") + "\n\n")
	if qr != "" {
		b.WriteString(indent(qr, 2) + "\n\n")
	}
	b.WriteString("  " + dimStyle.Render(label) + "\n")
	b.WriteString("  " + accentStyle.Render(url) + "\n")
	b.WriteString("\n  " + dimStyle.Render("pulse claude · codex · opencode  starts a session") + "\n")
	b.WriteString("  " + dimStyle.Render("Ctrl-C  quits") + "\n")
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
