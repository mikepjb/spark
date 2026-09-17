package main

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	maxInputHeight = 6
	roleUser       = "user"
	roleAssistant  = "assistant"
)

var (
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Background(lipgloss.Color("235"))
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			PaddingLeft(1)
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			PaddingLeft(1)
	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2).
			Background(lipgloss.Color("235"))
)

type chatMessage struct {
	role    string
	content string
}

type keyMap struct {
	Submit        key.Binding
	Quit          key.Binding
	InsertNewline key.Binding
	Help          key.Binding
	Settings      key.Binding
	Close         key.Binding
	ScrollUp      key.Binding
	ScrollDown    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		InsertNewline: key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("ctrl+j", "new line"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Settings: key.NewBinding(
			key.WithKeys("ctrl+,"),
			key.WithHelp("ctrl+,", "settings"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+up"),
			key.WithHelp("pgup/ctrl+↑", "history up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+down"),
			key.WithHelp("pgdn/ctrl+↓", "history down"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.InsertNewline, k.Help, k.Settings, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.InsertNewline, k.Help, k.Settings},
		{k.ScrollUp, k.ScrollDown, k.Close, k.Quit},
	}
}

type model struct {
	history  []chatMessage
	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model
	help     help.Model
	keys     keyMap

	busy         bool
	showHelp     bool
	showSettings bool
	modelName    string
	contextUsed  int
	contextLimit int
	windowWidth  int
	windowHeight int
}

func initialModel() model {
	keys := newKeyMap()
	input := textarea.New()
	input.Placeholder = "Send a message..."
	input.Prompt = "› "
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = maxInputHeight
	input.ShowLineNumbers = false
	input.SetVirtualCursor(false)
	input.KeyMap.InsertNewline.SetKeys(keys.InsertNewline.Keys()...)
	input.KeyMap.InsertNewline.SetHelp("ctrl+j", "new line")
	input.Focus()

	return model{
		history: []chatMessage{
			{role: roleAssistant, content: "Spark joy!"},
		},
		input:     input,
		viewport:  viewport.New(),
		spinner:   spinner.New(spinner.WithSpinner(spinner.Dot)),
		help:      help.New(),
		keys:      keys,
		modelName: "not connected",
	}
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.MouseWheelMsg:
		if m.showHelp || m.showSettings {
			return m, nil
		}
		m.viewport, _ = m.viewport.Update(msg)
		return m, nil

	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.Quit) {
			// Render one final frame without the real cursor so Bubble Tea can
			// restore the terminal cursor state on shutdown.
			m.input.Blur()
			return m, tea.Quit
		}

		if m.showSettings {
			if key.Matches(msg, m.keys.Close) || key.Matches(msg, m.keys.Settings) {
				m.closeOverlay()
			}
			return m, nil
		}

		if m.showHelp {
			if key.Matches(msg, m.keys.Close) || key.Matches(msg, m.keys.Help) {
				m.closeOverlay()
			}
			return m, nil
		}

		if key.Matches(msg, m.keys.Settings) {
			m.showSettings = true
			m.input.Blur()
			return m, nil
		}

		// Keep '?' available as normal chat input once the user has started typing.
		if key.Matches(msg, m.keys.Help) && strings.TrimSpace(m.input.Value()) == "" {
			m.showHelp = true
			m.help.ShowAll = true
			m.input.Blur()
			m.resize(m.windowWidth, m.windowHeight)
			return m, nil
		}

		if key.Matches(msg, m.keys.Submit) {
			message := strings.TrimSpace(m.input.Value())
			if message == "" {
				return m, nil
			}

			m.history = append(m.history, chatMessage{role: roleUser, content: message})
			m.input.Reset()
			m.refreshHistory(true)
			return m, nil
		}

		if key.Matches(msg, m.keys.ScrollUp) || key.Matches(msg, m.keys.ScrollDown) {
			m.viewport, _ = m.viewport.Update(msg)
			return m, nil
		}

		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.resize(m.windowWidth, m.windowHeight)
		return m, cmd

	case cursor.BlinkMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *model) resize(width, height int) {
	m.windowWidth = width
	m.windowHeight = height
	m.help.SetWidth(width)

	// The input box contributes two rows for its top and bottom borders.
	m.input.SetWidth(atLeastOne(width - 2))

	footerHeight := lipgloss.Height(m.statusView())
	if m.showHelp {
		footerHeight += lipgloss.Height(m.help.View(m.keys))
	}

	m.viewport.SetWidth(width)
	m.viewport.SetHeight(atLeastOne(height - m.input.Height() - footerHeight - 2))
	m.refreshHistory(false)
}

func (m *model) refreshHistory(forceBottom bool) {
	atBottom := forceBottom || m.viewport.AtBottom()

	var messages []string
	for _, message := range m.history {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		if message.role == roleUser {
			style = style.Foreground(lipgloss.Color("86"))
		}
		messages = append(messages, style.Render(message.content))
	}

	content := strings.Join(messages, "\n\n")
	content = lipgloss.NewStyle().Width(m.viewport.Width()).Render(content)
	m.viewport.SetContent(content)
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m model) statusView() string {
	state := "ready"
	activity := ""
	if m.busy {
		state = "thinking"
		activity = m.spinner.View() + " "
	}

	context := "—"
	if m.contextLimit > 0 {
		context = fmt.Sprintf("%d/%d", m.contextUsed, m.contextLimit)
	}

	return statusStyle.Render(fmt.Sprintf("%smodel: %s · context: %s · %s", activity, m.modelName, context, state))
}

func (m *model) closeOverlay() {
	m.showSettings = false
	m.showHelp = false
	m.input.Focus()
	m.resize(m.windowWidth, m.windowHeight)
}

func (m model) View() tea.View {
	if m.showSettings {
		return m.overlayView("Settings", "Model: "+m.modelName, "Context: not connected", "Press esc to close")
	}

	historyView := m.viewport.View()
	inputView := inputBoxStyle.Render(m.input.View())
	statusView := m.statusView()

	parts := []string{historyView, inputView, statusView}
	if m.showHelp {
		parts = append(parts, helpStyle.Render(m.help.View(m.keys)))
	}

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, parts...))

	// The real cursor needs the history and footer heights added to its local Y.
	if cursor := m.input.Cursor(); cursor != nil {
		cursor.X++
		cursor.Y += lipgloss.Height(historyView) + 1
		v.Cursor = cursor
	}

	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) overlayView(title string, lines ...string) tea.View {
	content := append([]string{title}, lines...)
	modal := modalStyle.Render(strings.Join(content, "\n"))
	v := tea.NewView(lipgloss.Place(m.windowWidth, m.windowHeight, lipgloss.Center, lipgloss.Center, modal))
	v.AltScreen = true
	return v
}

func atLeastOne(value int) int {
	if value < 1 {
		return 1
	}
	return value
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
