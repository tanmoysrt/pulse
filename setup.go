package main

import (
	"fmt"
	"os"
	"slices"
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
		{"Let's Encrypt", "a trusted HTTPS cert on your own domain or IP — recommended for a VPS"},
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
	m := wizModel{steps: []string{"saved"}, o: o, input: newWizardInput(), saved: saved}
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

	m := wizModel{steps: steps, o: o, input: newWizardInput()}
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

func newWizardInput() textinput.Model {
	ti := textinput.New()
	ti.CharLimit = 64
	ti.Width = 34
	return ti
}

func setupSteps(o opts) []string {
	var steps []string
	if !o.local && !o.tunnelSet && !o.acmeSet {
		steps = append(steps, "expose")
	}
	if !o.passwordSet {
		steps = append(steps, "password")
	}
	if !o.notifySet {
		steps = append(steps, "notify")
	}
	return steps
}

func hasSetupOverrides(o opts) bool {
	return o.local || o.tunnelSet || o.acmeSet || o.notifySet || o.passwordSet || o.portSet
}

func applySavedSetup(o opts) opts {
	saved, err := readSetup()
	if err != nil {
		return o
	}
	return applySetup(o, saved)
}

func applySetup(o opts, saved *setupRecord) opts {
	if !o.local && !o.tunnelSet && !o.acmeSet {
		o.tunnel = saved.Tunnel
		o.acme = saved.Acme
		o.domain = saved.Domain
	}
	if !o.notifySet {
		o.localNotify = saved.Notify
	}
	if !o.passwordSet {
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
	error  string
}

func (m wizModel) Init() tea.Cmd { return textinput.Blink }

func (m *wizModel) focusStep() {
	m.input.Reset()
	switch m.step() {
	case "password":
		m.input.Placeholder = "required"
		m.input.Focus()
	case "domain":
		m.input.Placeholder = "your-domain.com or 203.0.113.5"
		if m.saved != nil && m.saved.Domain != "" {
			m.input.SetValue(m.saved.Domain)
		}
		m.input.Focus()
	default:
		m.input.Blur()
	}
	m.cursor = 0
}

func (m wizModel) step() string { return m.steps[m.i] }

// isTextStep reports whether the current step is a free-text input rather
// than a list of choices — cursor movement and up/down navigation don't apply.
func (m wizModel) isTextStep() bool {
	return m.step() == "password" || m.step() == "domain"
}

// maxCursor is the highest selectable choice index for the current step.
func (m wizModel) maxCursor() int {
	if m.step() == "expose" {
		return len(exposeChoices) - 1
	}
	return 1
}

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
		if !m.isTextStep() && m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if !m.isTextStep() && m.cursor < m.maxCursor() {
			m.cursor++
		}
	case "enter":
		return m.commit()
	}
	if m.isTextStep() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// commit records the current step's answer and advances (or quits on the last).
func (m wizModel) commit() (tea.Model, tea.Cmd) {
	m.error = ""
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
		m.o.acme = m.cursor == 2
		if m.o.acme {
			m.steps = insertDomainStep(m.steps, m.i)
		}
	case "domain":
		v := strings.TrimSpace(m.input.Value())
		if v == "" {
			m.error = "Enter the domain or IP this server is reachable at."
			return m, nil
		}
		if err := validateExposeTarget(v); err != nil {
			m.error = err.Error()
			return m, nil
		}
		m.o.domain = v
	case "password":
		m.o.password = strings.TrimSpace(m.input.Value())
		if m.o.password == "" {
			m.error = "Enter a login password to continue."
			return m, nil
		}
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

// insertDomainStep adds a "domain" step right after index i, unless it's
// already there (e.g. the user flipped between expose choices).
func insertDomainStep(steps []string, i int) []string {
	if slices.Contains(steps, "domain") {
		return steps
	}
	out := append([]string{}, steps[:i+1]...)
	out = append(out, "domain")
	return append(out, steps[i+1:]...)
}

func (m wizModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n", pulseWordmark(), dimStyle.Render(fmt.Sprintf("setup · %d of %d", m.i+1, len(m.steps))))

	switch m.step() {
	case "saved":
		b.WriteString(titleStyle.Render("Use saved setup?"))
		b.WriteString("\n\n")
		b.WriteString(m.renderChoices([]choice{{"Start Pulse", "continue with saved setup"}, {"Redo setup", "choose everything again"}}))
	case "expose":
		b.WriteString(titleStyle.Render("How should pulse be reachable?"))
		b.WriteString("\n\n")
		b.WriteString(m.renderChoices(exposeChoices))
	case "domain":
		b.WriteString(titleStyle.Render("Which domain or IP is this server reachable at?"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Pulse will request a trusted HTTPS certificate for it from Let's Encrypt."))
		b.WriteString("\n\n  ")
		b.WriteString(m.input.View())
		b.WriteString("\n")
	case "password":
		b.WriteString(titleStyle.Render("Set a login password"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Required for the login page; scanning the QR skips it."))
		b.WriteString("\n\n  ")
		b.WriteString(m.input.View())
		b.WriteString("\n")
	case "notify":
		b.WriteString(titleStyle.Render("Desktop notifications on this machine?"))
		b.WriteString("\n\n")
		b.WriteString(m.renderChoices(notifyChoices))
	}
	if m.error != "" {
		b.WriteString("\n")
		b.WriteString(m.error)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑/↓ move · enter confirm · esc cancel"))
	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func (m wizModel) renderChoices(choices []choice) string {
	var b strings.Builder
	for i, c := range choices {
		if i == m.cursor {
			b.WriteString(accentStyle.Render("  › " + c.label))
			b.WriteString("  ")
			b.WriteString(dimStyle.Render(c.desc))
			b.WriteString("\n")
		} else {
			b.WriteString(dimStyle.Render("    " + c.label + "  " + c.desc))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderSummary keeps the connection details beside the QR so both scan and
// command options are visible without scrolling. footer is the closing dim
// line — "Ctrl-C quits" while attached, something else when just reprinting
// status for an already-running daemon.
func renderSummary(urls []string, qr, footer string) string {
	left := strings.Builder{}
	left.WriteString(pulseWordmark())
	left.WriteString("\n\n")
	left.WriteString(dimStyle.Render("URLs:"))
	left.WriteString("\n")
	for _, url := range urls {
		left.WriteString("- ")
		left.WriteString(accentStyle.Render(url))
		left.WriteString("\n")
	}
	left.WriteString("\n")
	left.WriteString(dimStyle.Render("Commands:"))
	left.WriteString("\n")
	left.WriteString("- pulse claude\n- pulse opencode\n- pulse codex\n- pulse ls\n- pulse attach <id>\n- pulse update\n- pulse version\n\n")
	left.WriteString(dimStyle.Render(footer))

	if qr == "" {
		return "\n" + left.String() + "\n"
	}
	return "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left.String(), "          ", "\n"+strings.TrimRight(qr, "\n")) + "\n"
}

// pulseWordmark is a terminal-safe rendering of the Pulse name.
func pulseWordmark() string {
	return accentStyle.Render(` ____  _   _ _     ____  _____
|  _ \| | | | |   / ___|| ____|
| |_) | | | | |   \___ \|  _|
|  __/| |_| | |___ ___) | |___
|_|    \___/|_____|____/|_____|`)
}
