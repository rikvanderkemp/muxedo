// SPDX-License-Identifier: MIT

// Package welcome implements the first-run wizard that authors a muxedo
// profile when no -profile flag was passed and ./.muxedo is missing. The
// wizard is a standalone Bubble Tea program; on successful completion it
// writes the collected configuration to disk and returns the saved path so
// the main program can load it and continue.
package welcome

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rikvanderkemp/muxedo/internal/process"
	"github.com/rikvanderkemp/muxedo/internal/profile"
	"github.com/rikvanderkemp/muxedo/internal/ui"
)

type step int

const (
	stepIntro step = iota
	stepTitle
	stepWorkingDir
	stepStartupCmd
	stepStartupMode
	stepStartupMore
	stepPanelName
	stepPanelWorkingDir
	stepPanelCmd
	stepPanelMore
	stepSavePath
	stepConfirm
	stepSaved
)

// Result is returned to the caller after the wizard exits.
type Result struct {
	SavedPath string
	Aborted   bool
}

// Model is the Bubble Tea model driving the wizard.
type Model struct {
	step   step
	theme  ui.Theme
	width  int
	height int

	input  textinput.Model
	errMsg string

	dirMatches  []string
	dirPickOpen bool
	dirPickIdx  int

	profile      WizardProfile
	pendingCmd   Startup
	pendingPanel Panel

	modeChoice profile.StartupMode
	moreChoice bool

	aborted   bool
	savedPath string

	cwd string
}

// DefaultSaveFile is the filename used when the user does not override the
// save path (relative to cwd).
const DefaultSaveFile = ".muxedo"

// New creates a wizard model rooted at cwd using the supplied theme.
func New(cwd string, theme ui.Theme) Model {
	m := Model{
		theme:      theme,
		cwd:        cwd,
		modeChoice: profile.StartupModeAsync,
		moreChoice: false,
		profile: WizardProfile{
			WorkingDir: ".",
		},
	}
	m.input = m.newInput("", "")
	return m
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Run runs the wizard via runProgram and returns a Result describing the
// outcome. runProgram should mirror main.runProgram (tea.NewProgram with the
// usual options).
func Run(cwd string, theme ui.Theme, runProgram func(tea.Model) (tea.Model, error)) (Result, error) {
	m := New(cwd, theme)
	finalModel, err := runProgram(m)
	if err != nil {
		return Result{}, err
	}
	fm, ok := finalModel.(Model)
	if !ok {
		return Result{}, fmt.Errorf("welcome: unexpected model type %T", finalModel)
	}
	return Result{SavedPath: fm.savedPath, Aborted: fm.aborted}, nil
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.KeyMsg:
		// Ignore key releases and other key events.
		return m, nil
	}

	var cmd tea.Cmd
	if m.usesInput() {
		m.input, cmd = m.input.Update(msg)
	}
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.aborted = true
		return m, tea.Quit
	}

	if m.step == stepSaved {
		if msg.String() == "enter" || msg.String() == "space" {
			m.aborted = false
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		if m.isDirStep() && m.dirPickOpen {
			m.closeDirPicker()
			return m, nil
		}
		return m.goBack()
	case "enter":
		return m.advance()
	}

	if m.isChoiceStep() {
		return m.handleChoiceKey(msg)
	}

	if m.isDirStep() {
		switch msg.Code {
		case tea.KeyTab:
			if m.dirPickOpen && len(m.dirMatches) > 0 {
				m.replaceInput(m.dirMatches[m.dirPickIdx])
				m.closeDirPicker()
				return m, nil
			}
			m.handleDirTab()
			return m, nil
		case tea.KeyUp:
			if m.dirPickOpen && len(m.dirMatches) > 0 {
				m.dirPickIdx = (m.dirPickIdx - 1 + len(m.dirMatches)) % len(m.dirMatches)
				return m, nil
			}
		case tea.KeyDown:
			if m.dirPickOpen && len(m.dirMatches) > 0 {
				m.dirPickIdx = (m.dirPickIdx + 1) % len(m.dirMatches)
				return m, nil
			}
		}
	}

	if m.usesInput() {
		// Backspace on an empty field jumps back a step for a nicer UX.
		if msg.String() == "backspace" && m.input.Value() == "" {
			return m.goBack()
		}
		m.errMsg = ""
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.isDirStep() {
			m.closeDirPicker()
		}
		return m, cmd
	}

	return m, nil
}

func (m Model) isChoiceStep() bool {
	return m.step == stepStartupMode || m.step == stepStartupMore || m.step == stepPanelMore
}

func (m Model) usesInput() bool {
	switch m.step {
	case stepTitle, stepWorkingDir, stepStartupCmd, stepPanelName, stepPanelWorkingDir, stepPanelCmd, stepSavePath:
		return true
	}
	return false
}

func (m Model) isDirStep() bool {
	return m.step == stepWorkingDir || m.step == stepPanelWorkingDir
}

// closeDirPicker hides the working directory dropdown and clears cached
// matches. Called whenever the user edits the input so the picker only
// surfaces after an explicit Tab press.
func (m *Model) closeDirPicker() {
	m.dirMatches = nil
	m.dirPickOpen = false
	m.dirPickIdx = 0
}

// handleDirTab implements shell-like Tab completion for working dir inputs:
//   - extends the input to the longest common prefix of all matching
//     directories,
//   - when already at the common prefix and there are multiple candidates,
//     opens the dropdown picker.
func (m *Model) handleDirTab() {
	matches := dirSuggestions(m.cwd, m.input.Value())
	if len(matches) == 0 {
		m.closeDirPicker()
		m.errMsg = "no matching directory"
		return
	}
	if len(matches) == 1 {
		m.replaceInput(matches[0])
		m.closeDirPicker()
		m.errMsg = ""
		return
	}

	lcp := longestCommonPrefix(matches)
	if lcp != "" && len(lcp) > len(m.input.Value()) {
		m.replaceInput(lcp)
		m.dirMatches = matches
		m.dirPickOpen = false
		m.dirPickIdx = 0
		m.errMsg = ""
		return
	}

	m.dirMatches = matches
	m.dirPickOpen = true
	if m.dirPickIdx >= len(matches) {
		m.dirPickIdx = 0
	}
	m.errMsg = ""
}

func (m *Model) replaceInput(value string) {
	m.input.SetValue(value)
	m.input.CursorEnd()
}

func (m Model) handleChoiceKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	isToggleKey := msg.String() == "space" || isToggleString(msg.String())

	switch m.step {
	case stepStartupMode:
		switch strings.ToLower(msg.String()) {
		case "a":
			m.modeChoice = profile.StartupModeAsync
			return m, nil
		case "s":
			m.modeChoice = profile.StartupModeSync
			return m, nil
		}
		if isToggleKey {
			if m.modeChoice == profile.StartupModeAsync {
				m.modeChoice = profile.StartupModeSync
			} else {
				m.modeChoice = profile.StartupModeAsync
			}
		}
	case stepStartupMore, stepPanelMore:
		switch strings.ToLower(msg.String()) {
		case "y":
			m.moreChoice = true
			return m.advance()
		case "n":
			m.moreChoice = false
			return m.advance()
		}
		if isToggleKey {
			m.moreChoice = !m.moreChoice
		}
	}
	return m, nil
}

func isToggleString(s string) bool {
	switch strings.ToLower(s) {
	case "left", "right", "tab", "shift+tab", "h", "l":
		return true
	}
	return false
}

func (m Model) newInput(placeholder, initial string) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.Prompt = "> "
	in.CharLimit = 512
	in.SetWidth(60)

	styles := textinput.DefaultStyles(false)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.InactiveTitleFG))
	styles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.StatusTimeFG))
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.StatusHintFG)).Faint(true)
	styles.Blurred.Prompt = styles.Focused.Prompt
	styles.Blurred.Text = styles.Focused.Text
	styles.Blurred.Placeholder = styles.Focused.Placeholder
	in.SetStyles(styles)

	in.SetValue(initial)
	in.CursorEnd()
	_ = in.Focus()
	return in
}

func (m Model) advance() (tea.Model, tea.Cmd) {
	m.errMsg = ""
	m.closeDirPicker()
	switch m.step {
	case stepIntro:
		m.step = stepTitle
		m.input = m.newInput(profile.RandomName(), "")
	case stepTitle:
		m.profile.Title = strings.TrimSpace(m.input.Value())
		if m.profile.Title == "" {
			m.profile.Title = m.input.Placeholder
		}
		m.step = stepWorkingDir
		m.input = m.newInput(".", "")
	case stepWorkingDir:
		wd := strings.TrimSpace(m.input.Value())
		if wd == "" {
			wd = "."
		}
		if _, err := workingDirExists(m.cwd, wd); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.profile.WorkingDir = wd
		m.step = stepStartupCmd
		m.input = m.newInput("e.g. printf 'ready\\n' (leave empty to skip)", "")
	case stepStartupCmd:
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			m.step = stepPanelName
			m.input = m.newInput("e.g. dev server", "")
			return m, textinput.Blink
		}
		cmd := ParseCommand(raw)
		m.pendingCmd = Startup{Command: cmd, Mode: profile.StartupModeAsync}
		m.modeChoice = profile.StartupModeAsync
		m.step = stepStartupMode
	case stepStartupMode:
		m.pendingCmd.Mode = m.modeChoice
		m.profile.Startup = append(m.profile.Startup, m.pendingCmd)
		m.pendingCmd = Startup{}
		m.moreChoice = false
		m.step = stepStartupMore
	case stepStartupMore:
		if m.moreChoice {
			m.step = stepStartupCmd
			m.input = m.newInput("e.g. npm install", "")
			return m, textinput.Blink
		}
		m.step = stepPanelName
		m.input = m.newInput("e.g. dev server", "")
	case stepPanelName:
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			m.errMsg = "panel name is required"
			return m, nil
		}
		slug := Slugify(raw)
		if m.slugTaken(slug) {
			m.errMsg = fmt.Sprintf("slug %q already used; pick another name", slug)
			return m, nil
		}
		m.pendingPanel = Panel{Slug: slug}
		m.step = stepPanelWorkingDir
		m.input = m.newInput(m.globalWorkingDirPlaceholder(), "")
	case stepPanelWorkingDir:
		raw := strings.TrimSpace(m.input.Value())
		if raw != "" {
			if _, err := workingDirExists(m.cwd, raw); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
		}
		m.pendingPanel.WorkingDir = raw
		m.step = stepPanelCmd
		m.input = m.newInput(`e.g. npm run dev -f --arguments=2123`, "")
	case stepPanelCmd:
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			m.errMsg = "command is required"
			return m, nil
		}
		m.pendingPanel.Command = ParseCommand(raw)
		m.profile.Panels = append(m.profile.Panels, m.pendingPanel)
		m.pendingPanel = Panel{}
		m.moreChoice = false
		m.step = stepPanelMore
	case stepPanelMore:
		if m.moreChoice {
			m.step = stepPanelName
			m.input = m.newInput("e.g. logs", "")
			return m, textinput.Blink
		}
		if len(m.profile.Panels) == 0 {
			m.errMsg = "at least one panel is required"
			m.moreChoice = true
			return m, nil
		}
		m.step = stepSavePath
		def := filepath.Join(m.cwd, DefaultSaveFile)
		m.input = m.newInput(def, def)
	case stepSavePath:
		path := strings.TrimSpace(m.input.Value())
		if path == "" {
			path = filepath.Join(m.cwd, DefaultSaveFile)
		}
		path = expandHome(path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(m.cwd, path)
		}
		m.savedPath = path
		m.step = stepConfirm
	case stepConfirm:
		savedPath, err := m.writeProfile()
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.savedPath = savedPath
		m.step = stepSaved
		return m, nil
	default:
		return m, textinput.Blink
	}
	return m, textinput.Blink
}

func (m Model) goBack() (tea.Model, tea.Cmd) {
	m.errMsg = ""
	m.closeDirPicker()
	switch m.step {
	case stepIntro:
		m.aborted = true
		return m, tea.Quit
	case stepTitle:
		m.step = stepIntro
	case stepWorkingDir:
		m.step = stepTitle
		m.input = m.newInput(profile.RandomName(), m.profile.Title)
	case stepStartupCmd:
		m.step = stepWorkingDir
		m.input = m.newInput(".", m.profile.WorkingDir)
	case stepStartupMode:
		m.step = stepStartupCmd
		m.input = m.newInput("e.g. printf 'ready\\n' (leave empty to skip)", commandText(m.pendingCmd.Command))
	case stepStartupMore:
		if n := len(m.profile.Startup); n > 0 {
			last := m.profile.Startup[n-1]
			m.profile.Startup = m.profile.Startup[:n-1]
			m.modeChoice = last.Mode
			m.pendingCmd = last
			m.step = stepStartupMode
		} else {
			m.step = stepStartupCmd
			m.input = m.newInput("e.g. printf 'ready\\n' (leave empty to skip)", "")
		}
	case stepPanelName:
		if n := len(m.profile.Startup); n > 0 {
			m.step = stepStartupMore
			m.moreChoice = false
		} else {
			m.step = stepStartupCmd
			m.input = m.newInput("e.g. printf 'ready\\n' (leave empty to skip)", "")
		}
	case stepPanelWorkingDir:
		m.step = stepPanelName
		m.input = m.newInput("e.g. dev server", m.pendingPanel.Slug)
	case stepPanelCmd:
		m.step = stepPanelWorkingDir
		m.input = m.newInput(m.globalWorkingDirPlaceholder(), m.pendingPanel.WorkingDir)
	case stepPanelMore:
		if n := len(m.profile.Panels); n > 0 {
			last := m.profile.Panels[n-1]
			m.profile.Panels = m.profile.Panels[:n-1]
			m.pendingPanel = last
			m.step = stepPanelCmd
			m.input = m.newInput(`e.g. npm run dev -f --arguments=2123`, commandText(last.Command))
		} else {
			m.step = stepPanelName
			m.input = m.newInput("e.g. dev server", "")
		}
	case stepSavePath:
		m.step = stepPanelMore
		m.moreChoice = false
	case stepConfirm:
		m.step = stepSavePath
		def := filepath.Join(m.cwd, DefaultSaveFile)
		m.input = m.newInput(def, m.savedPath)
	}
	return m, textinput.Blink
}

func (m Model) globalWorkingDirPlaceholder() string {
	wd := strings.TrimSpace(m.profile.WorkingDir)
	if wd == "" {
		wd = "."
	}
	return fmt.Sprintf("inherit global (%s)", wd)
}

func (m Model) resolvedPanelWorkingDir(raw string) string {
	raw = strings.TrimSpace(raw)
	global := strings.TrimSpace(m.profile.WorkingDir)
	if global == "" {
		global = "."
	}
	if raw == "" {
		return fmt.Sprintf("inherit %s", global)
	}
	return raw
}

func (m Model) slugTaken(slug string) bool {
	for _, p := range m.profile.Panels {
		if p.Slug == slug {
			return true
		}
	}
	return false
}

func (m Model) writeProfile() (string, error) {
	out := RenderTOML(m.profile)
	savedPath := m.savedPath
	if savedPath == "" {
		savedPath = filepath.Join(m.cwd, DefaultSaveFile)
	}
	dir := filepath.Dir(savedPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	f, err := os.OpenFile(savedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("refusing to overwrite existing profile %s", savedPath)
		}
		return "", fmt.Errorf("creating profile %s: %w", savedPath, err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(out)); err != nil {
		return "", fmt.Errorf("writing profile %s: %w", savedPath, err)
	}
	return savedPath, nil
}

func (m Model) view(content string) tea.View {
	content = strings.TrimSuffix(content, "\n")
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Muxedo - first run"
	return v
}

// View satisfies tea.Model.
func (m Model) View() tea.View {
	if m.step == stepSaved {
		return m.view(m.renderSaved())
	}

	header := m.renderHeader()
	body := m.renderBody()
	footer := m.renderFooter()

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.ActiveNormalBorder)).
		Padding(2, 3).
		Width(clampWidth(m.width))

	card := cardStyle.Render(strings.Join([]string{header, "", "", body, "", footer}, "\n"))

	if m.width == 0 || m.height == 0 {
		return m.view(card)
	}
	return m.view(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card))
}

func clampWidth(w int) int {
	const (
		minWidth = 60
		maxWidth = 96
	)
	if w <= 0 {
		return 72
	}
	target := w - 6
	if target < minWidth {
		return minWidth
	}
	if target > maxWidth {
		return maxWidth
	}
	return target
}

func (m Model) renderHeader() string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.ActiveNormalTitleFG)).
		Background(lipgloss.Color(m.theme.ActiveNormalTitleBG)).
		Bold(true).
		Padding(0, 1).
		Render(" muxedo · first run ")

	stepLabel := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.StatusHintFG)).
		Render(m.stepIndicator())

	return lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", stepLabel)
}

func (m Model) stepIndicator() string {
	const total = 7
	var n int
	switch m.step {
	case stepIntro:
		n = 1
	case stepTitle:
		n = 2
	case stepWorkingDir:
		n = 3
	case stepStartupCmd, stepStartupMode, stepStartupMore:
		n = 4
	case stepPanelName, stepPanelWorkingDir, stepPanelCmd, stepPanelMore:
		n = 5
	case stepSavePath:
		n = 6
	case stepConfirm:
		n = 7
	case stepSaved:
		n = total
	default:
		n = total
	}
	return fmt.Sprintf("step %d of %d", n, total)
}

func (m Model) renderBody() string {
	var heading, prompt, body, preview, helper string

	switch m.step {
	case stepIntro:
		heading = "Welcome to muxedo"
		prompt = "No profile found in this directory."
		body = introBodyStyle(m.theme).Render(strings.Join([]string{
			"This wizard builds a minimal .muxedo for you: a title, a working directory,",
			"optional startup commands, and one or more panels.",
			"",
			"Press Enter to begin, or Esc / Ctrl+C to exit.",
		}, "\n"))
	case stepTitle:
		heading = "Profile title"
		prompt = "What should this profile be called?"
		body = wrapField(m.theme, m.input.View())
		helper = "A random name is used if you leave it empty."
	case stepWorkingDir:
		heading = "Working directory"
		prompt = "Where should panels run by default?"
		body = wrapField(m.theme, m.input.View())
		if picker := m.renderDirPicker(); picker != "" {
			body += "\n" + picker
		}
		helper = `Tab completes · ↑/↓ to pick · "." is current · "~" expands to home.`
	case stepStartupCmd:
		heading = "Startup command"
		if n := len(m.profile.Startup); n > 0 {
			prompt = fmt.Sprintf("Add another startup command (%d saved) or leave empty to continue.", n)
		} else {
			prompt = "Add a command that runs before panels (optional)."
		}
		body = wrapField(m.theme, m.input.View())
		preview = m.formatCommandPreview(m.input.Value())
		helper = "Example: npm install, or: printf 'ready\\n'"
	case stepStartupMode:
		heading = "Startup mode"
		prompt = "Should startup tasks after this one wait for it to finish?"
		body = wrapField(m.theme, m.renderToggle(
			"async (fire and forget)",
			"sync (block until done)",
			m.modeChoice == profile.StartupModeSync,
		))
		helper = "Tab / ←→ to switch · Enter to confirm"
	case stepStartupMore:
		heading = "Another startup command?"
		prompt = fmt.Sprintf("%d startup command(s) saved.", len(m.profile.Startup))
		body = wrapField(m.theme, m.renderToggle("no, continue", "yes, add another", m.moreChoice))
		helper = "y / n · Tab to switch · Enter to confirm"
	case stepPanelName:
		heading = fmt.Sprintf("Panel %d · name", len(m.profile.Panels)+1)
		prompt = "Give this panel a short, memorable name."
		body = wrapField(m.theme, m.input.View())
		if v := strings.TrimSpace(m.input.Value()); v != "" {
			preview = m.previewHint(fmt.Sprintf("toml section: [panel.%s]", Slugify(v)))
		}
		helper = "Names are slugged for TOML (lowercase, '-' separated)."
	case stepPanelWorkingDir:
		heading = fmt.Sprintf("Panel %d · working directory", len(m.profile.Panels)+1)
		prompt = fmt.Sprintf("Where should [panel.%s] run? Leave empty to use the global default.", m.pendingPanel.Slug)
		body = wrapField(m.theme, m.input.View())
		if picker := m.renderDirPicker(); picker != "" {
			body += "\n" + picker
		}
		preview = m.previewHint("→ " + m.resolvedPanelWorkingDir(m.input.Value()))
		helper = `Tab completes · ↑/↓ to pick · "." is current · "~" expands to home.`
	case stepPanelCmd:
		heading = fmt.Sprintf("Panel %d · command", len(m.profile.Panels)+1)
		prompt = fmt.Sprintf("What should [panel.%s] run?", m.pendingPanel.Slug)
		body = wrapField(m.theme, m.input.View())
		preview = m.formatCommandPreview(m.input.Value())
		helper = "Type the full command line. Pipes, redirects, and quotes emit shell = \"…\"."
	case stepPanelMore:
		heading = "Another panel?"
		prompt = fmt.Sprintf("%d panel(s) saved.", len(m.profile.Panels))
		body = wrapField(m.theme, m.renderToggle("no, continue", "yes, add another", m.moreChoice))
		helper = "y / n · Tab to switch · Enter to confirm"
	case stepSavePath:
		heading = "Save location"
		prompt = "Where should this profile be written?"
		body = wrapField(m.theme, m.input.View())
		resolved := resolvedSavePath(m.cwd, m.input.Value())
		preview = m.previewHint("→ " + resolved)
		helper = "muxedo loads ./.muxedo automatically. Ctrl+U to clear the field."
	case stepConfirm:
		heading = "Review and save"
		prompt = "This is the TOML that will be written:"
		preview = m.renderTomlPreview()
		helper = fmt.Sprintf("Enter to write %s · Esc to edit the path", m.savedPath)
	}

	parts := []string{headingStyle(m.theme).MarginBottom(1).Render(heading)}
	if prompt != "" {
		parts = append(parts, "", promptStyle(m.theme).Render(prompt))
	}
	if body != "" {
		parts = append(parts, "", body)
	}
	if preview != "" {
		parts = append(parts, "", preview)
	}
	if m.errMsg != "" {
		parts = append(parts, "", errorStyle(m.theme).Render("✗ "+m.errMsg))
	}
	if helper != "" {
		parts = append(parts, "", helperStyle(m.theme).Render(helper))
	}
	return strings.Join(parts, "\n")
}

func (m Model) renderToggle(leftLabel, rightLabel string, rightSelected bool) string {
	active := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.ActiveNormalTitleFG)).
		Background(lipgloss.Color(m.theme.ActiveNormalTitleBG)).
		Bold(true).
		Padding(0, 2)
	inactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.StatusHintFG)).
		Padding(0, 2)

	left := inactive.Render(leftLabel)
	right := inactive.Render(rightLabel)
	if rightSelected {
		right = active.Render(rightLabel)
	} else {
		left = active.Render(leftLabel)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func (m Model) renderTomlPreview() string {
	toml := RenderTOML(m.profile)
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.theme.ActiveNormalBorder)).
		Background(lipgloss.Color(m.theme.StatusBarBG)).
		Padding(1, 1)
	return box.Render(toml)
}

func (m Model) renderFooter() string {
	return helperStyle(m.theme).Render("Enter continue · Esc back · Ctrl+C cancel")
}

func (m Model) renderSaved() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.ActiveInsertBorder)).
		Padding(1, 2)
	return style.Render(fmt.Sprintf("Wrote profile to %s\nLaunching muxedo…", m.savedPath))
}

func headingStyle(t ui.Theme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.ActiveNormalTitleFG)).
		Bold(true)
}

func promptStyle(t ui.Theme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.StatusBarFG)).
		Bold(true)
}

func hintStyle(t ui.Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(t.StatusHintFG))
}

func helperStyle(t ui.Theme) lipgloss.Style {
	return hintStyle(t)
}

func errorStyle(t ui.Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(t.StoppedTitleFG)).Bold(true)
}

func introBodyStyle(t ui.Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(t.InactiveTitleFG))
}

func wrapField(t ui.Theme, inner string) string {
	if inner == "" {
		return ""
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(t.InactiveBorder)).
		Background(lipgloss.Color(t.StatusBarBG)).
		Padding(0, 1).
		Render(inner)
}

func (m Model) previewHint(text string) string {
	return hintStyle(m.theme).Render(text)
}

// renderDirPicker renders the autocomplete dropdown list when it is open.
// Returns an empty string when the picker is closed or has no matches, so
// callers can unconditionally concatenate the result.
func (m Model) renderDirPicker() string {
	if !m.dirPickOpen || len(m.dirMatches) == 0 {
		return ""
	}

	const maxVisible = 8
	matches := m.dirMatches
	start := 0
	if len(matches) > maxVisible {
		// Keep the selected row on screen with a modest window around it.
		half := maxVisible / 2
		start = m.dirPickIdx - half
		if start < 0 {
			start = 0
		}
		if start+maxVisible > len(matches) {
			start = len(matches) - maxVisible
		}
	}
	end := start + maxVisible
	if end > len(matches) {
		end = len(matches)
	}

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.ActiveNormalTitleFG)).
		Background(lipgloss.Color(m.theme.ActiveNormalTitleBG)).
		Bold(true)
	normalStyle := hintStyle(m.theme)

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		label := matches[i]
		if i == m.dirPickIdx {
			rows = append(rows, selectedStyle.Render("› "+label))
		} else {
			rows = append(rows, normalStyle.Render("  "+label))
		}
	}
	if len(matches) > maxVisible {
		rows = append(rows, normalStyle.Render(fmt.Sprintf("  … %d more", len(matches)-maxVisible)))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.theme.InactiveBorder)).
		Padding(0, 1).
		Render(strings.Join(rows, "\n"))
	return box
}

func (m Model) formatCommandPreview(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	st := hintStyle(m.theme)
	cmd := ParseCommand(trimmed)
	if cmd.Shell != "" {
		return st.Render("→ shell = " + tomlDouble(cmd.Shell))
	}
	parts := []string{"→ program = " + tomlDouble(cmd.Program)}
	if len(cmd.Args) > 0 {
		quoted := make([]string, 0, len(cmd.Args))
		for _, a := range cmd.Args {
			quoted = append(quoted, tomlDouble(a))
		}
		parts = append(parts, "args = ["+strings.Join(quoted, ", ")+"]")
	}
	return st.Render(strings.Join(parts, "  "))
}

func tomlDouble(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	writeTOMLString(&b, s)
	return b.String()
}

func commandText(cmd process.CommandSpec) string {
	if cmd.Shell != "" {
		return cmd.Shell
	}
	if cmd.Program == "" {
		return ""
	}
	if len(cmd.Args) == 0 {
		return cmd.Program
	}
	var b strings.Builder
	b.Grow(len(cmd.Program) + len(cmd.Args)*8)
	b.WriteString(cmd.Program)
	for _, a := range cmd.Args {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	return b.String()
}

func resolvedSavePath(cwd, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return filepath.Join(cwd, DefaultSaveFile)
	}
	raw = expandHome(raw)
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(cwd, raw)
	}
	return raw
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
