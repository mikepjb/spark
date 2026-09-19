package view

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
	"github.com/mikepjb/spark/internal/commands"
	"github.com/mikepjb/spark/internal/llm"
	"github.com/mikepjb/spark/internal/repl"
)

type Backend interface {
	Submit(string) (uint64, error)
	Events() <-chan repl.Event
	APIHistory() []llm.Request
	Cancel()
	Close()
}

type submissionBackend interface {
	SubmitSubmission(repl.Submission) (uint64, error)
}

const (
	maxInputHeight        = 6
	historyExportFilename = "debug.log"
	roleUser              = "user"
	roleAssistant         = "assistant"
	roleTool              = "tool"
	exploreLabel          = "Explore"
)

type messageStatus uint8

const (
	statusCompleted messageStatus = iota
	statusActive
	statusSucceeded
	statusFailed
	statusCancelled
)

const (
	userMarker           = "›"
	assistantMarker      = "•"
	historyContentIndent = "  "
	promptBorderColor    = lipgloss.BrightBlack
	userMarkerColor      = "86"
	activeMarkerColor    = "255"
	completedMarkerColor = "244"
	succeededMarkerColor = "82"
	failedMarkerColor    = "196"
	cancelledMarkerColor = "214"
	workingColor         = "252"
	workingHintColor     = "241"
)

var (
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(promptBorderColor)
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
	id                   uint64
	role                 string
	toolID               string
	toolName             string
	content              string
	status               messageStatus
	startedAt            time.Time
	renderedContent      string
	renderedContentWidth int
	renderedContentValid bool
}

type replEventMsg struct {
	event repl.Event
}

type replClosedMsg struct{}

type keyMap struct {
	Submit           key.Binding
	Quit             key.Binding
	InsertNewline    key.Binding
	Help             key.Binding
	Settings         key.Binding
	Close            key.Binding
	Escape           key.Binding
	CompletionUp     key.Binding
	CompletionDown   key.Binding
	CompletionAccept key.Binding
	ScrollUp         key.Binding
	ScrollDown       key.Binding
	SaveHistory      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+q"),
			key.WithHelp("ctrl+q", "quit"),
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
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "close"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
		),
		CompletionUp: key.NewBinding(
			key.WithKeys("up", "ctrl+p"),
		),
		CompletionDown: key.NewBinding(
			key.WithKeys("down", "ctrl+n"),
		),
		CompletionAccept: key.NewBinding(
			key.WithKeys("tab", "right"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+up"),
			key.WithHelp("pgup/ctrl+↑", "history up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+down"),
			key.WithHelp("pgdn/ctrl+↓", "history down"),
		),
		SaveHistory: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("ctrl+x", "save history"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.InsertNewline, k.Help, k.Settings, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.InsertNewline, k.Help, k.Settings},
		{k.ScrollUp, k.ScrollDown, k.SaveHistory, k.Close, k.Quit},
	}
}

type model struct {
	history  []chatMessage
	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model
	help     help.Model
	keys     keyMap

	busy            bool
	queued          int
	cancelConfirm   bool
	showHelp        bool
	showSettings    bool
	notice          string
	activeID        uint64
	modelName       string
	contextUsed     int
	contextLimit    int
	windowWidth     int
	windowHeight    int
	markdown        markdownRenderer
	backend         Backend
	commands        *commands.Engine
	completion      commands.Completion
	completionIdx   int
	historyFilePath string
}

func inputStyles() textarea.Styles {
	styles := textarea.DefaultDarkStyles()

	unsetBackground := func(s *textarea.StyleState) {
		s.Base = s.Base.UnsetBackground()
		s.Prompt = s.Prompt.UnsetBackground()
		s.Text = s.Text.UnsetBackground()
		s.CursorLine = s.CursorLine.UnsetBackground()
		s.Placeholder = s.Placeholder.UnsetBackground()
		s.EndOfBuffer = s.EndOfBuffer.UnsetBackground()
	}

	unsetBackground(&styles.Focused)
	unsetBackground(&styles.Blurred)

	return styles
}

func newModel(backend Backend, modelName string, commandEngines ...*commands.Engine) model {
	keys := newKeyMap()
	input := textarea.New()
	input.Placeholder = "Send a message..."
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = maxInputHeight
	input.ShowLineNumbers = false
	input.SetVirtualCursor(false)
	input.KeyMap.InsertNewline.SetKeys(keys.InsertNewline.Keys()...)
	input.KeyMap.InsertNewline.SetHelp("ctrl+j", "new line")
	input.SetStyles(inputStyles())
	input.Focus()

	input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return "› "
		}
		return "  "
	})

	var commandEngine *commands.Engine
	if len(commandEngines) > 0 {
		commandEngine = commandEngines[0]
	}
	return model{
		history:   []chatMessage{},
		input:     input,
		viewport:  viewport.New(),
		spinner:   spinner.New(spinner.WithSpinner(spinner.Dot)),
		help:      help.New(),
		keys:      keys,
		modelName: modelName,
		backend:   backend,
		commands:  commandEngine,
	}
}

func Start(backend Backend, modelName string, contextLimit int, commandEngine *commands.Engine, workspace string) error {
	if backend != nil {
		defer backend.Close()
	}
	m := newModel(backend, modelName, commandEngine)
	m.historyFilePath = filepath.Join(workspace, historyExportFilename)
	if contextLimit > 0 {
		// The first context event will refresh this value with the server's
		// observed usage while the configured limit is useful immediately.
		m.contextLimit = contextLimit
	}
	p := tea.NewProgram(m)

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}

func (m model) Init() tea.Cmd {
	if m.backend == nil {
		return textarea.Blink
	}
	return tea.Batch(textarea.Blink, waitForEvent(m.backend))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case replEventMsg:
		return m, tea.Batch(m.handleReplEvent(msg.event), waitForEvent(m.backend))

	case replClosedMsg:
		return m, nil

	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.refreshHistory(false)
		return m, cmd

	case tea.MouseWheelMsg:
		if m.showHelp || m.showSettings {
			return m, nil
		}
		m.viewport, _ = m.viewport.Update(msg)
		return m, nil

	case tea.KeyPressMsg:
		if m.showSettings {
			if key.Matches(msg, m.keys.Close) || key.Matches(msg, m.keys.Escape) || key.Matches(msg, m.keys.Settings) {
				m.closeOverlay()
			}
			return m, nil
		}

		if m.showHelp {
			if key.Matches(msg, m.keys.Close) || key.Matches(msg, m.keys.Escape) || key.Matches(msg, m.keys.Help) {
				m.closeOverlay()
			}
			return m, nil
		}

		if m.cancelConfirm {
			if key.Matches(msg, m.keys.Close) {
				if m.backend != nil {
					m.backend.Cancel()
				}
				m.cancelConfirm = false
				m.input.Focus()
				return m, nil
			}
			if key.Matches(msg, m.keys.Escape) {
				m.cancelConfirm = false
				m.input.Focus()
				return m, nil
			}
			return m, nil
		}

		if key.Matches(msg, m.keys.Close) {
			if m.busy {
				m.cancelConfirm = true
				m.input.Blur()
				return m, nil
			}
			// Render one final frame without the real cursor so Bubble Tea can
			// restore the terminal cursor state on shutdown.
			m.input.Blur()
			return m, tea.Quit
		}

		if key.Matches(msg, m.keys.Quit) {
			m.input.Blur()
			return m, tea.Quit
		}

		updated, handledCmd, handled := m.handleKeyPress(msg)
		if handled {
			return updated, handledCmd
		}

		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.refreshCompletion()
		m.resize(m.windowWidth, m.windowHeight)
		return m, cmd

	case cursor.BlinkMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func waitForEvent(backend Backend) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-backend.Events()
		if !ok {
			return replClosedMsg{}
		}
		return replEventMsg{event: event}
	}
}

func (m *model) handleReplEvent(event repl.Event) tea.Cmd {
	switch event.Kind {
	case repl.EventQueued:
		m.queued = event.QueueCount
		m.history = append(m.history, chatMessage{id: event.RequestID, role: roleUser, content: event.Content})
		m.notice = ""
		m.refreshHistory(true)
	case repl.EventStarted:
		m.busy = true
		if m.queued > 0 {
			m.queued--
		}
		m.activeID = event.RequestID
		m.history = append(m.history, chatMessage{
			id:        event.RequestID,
			role:      roleAssistant,
			status:    statusActive,
			startedAt: time.Now(),
		})
		m.refreshHistory(true)
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(m.spinner.Tick())
		return cmd
	case repl.EventChunk:
		if message := m.assistantMessageForChunk(event.RequestID); message != nil {
			// Chunks may end inside any Markdown construct; always re-render the
			// accumulated source so later chunks can close it correctly.
			message.content += event.Content
			message.renderedContentValid = false
			m.refreshHistory(true)
		}
	case repl.EventCompleted:
		m.finishMessage(event.RequestID, statusSucceeded, "")
		m.busy = false
		m.activeID = 0
		m.notice = ""
	case repl.EventFailed:
		errorText := "request failed"
		if event.Err != nil {
			errorText = event.Err.Error()
		}
		m.finishMessage(event.RequestID, statusFailed, errorText)
		m.busy = false
		m.activeID = 0
		m.notice = errorText
	case repl.EventCancelled:
		m.finishMessage(event.RequestID, statusCancelled, "inference cancelled")
		m.busy = false
		m.activeID = 0
		m.queued = 0
		m.notice = ""
	case repl.EventContext:
		m.contextUsed = event.ContextUsed
		if event.ContextLimit > 0 {
			m.contextLimit = event.ContextLimit
		}
	case repl.EventToolStarted:
		m.history = append(m.history, chatMessage{
			id:       event.RequestID,
			role:     roleTool,
			toolID:   event.ToolCallID,
			toolName: event.Content,
			content:  event.Content,
			status:   statusActive,
		})
		m.refreshHistory(true)
	case repl.EventToolCompleted:
		if message := m.messageByToolID(event.RequestID, event.ToolCallID); message != nil {
			message.status = statusSucceeded
			if event.Failed {
				message.status = statusFailed
			}
			if event.Content != "" {
				message.content = event.Content
			}
			m.refreshHistory(true)
		}
	case repl.EventQueueCleared:
		m.queued = 0
		m.notice = "queued messages cleared"
	case repl.EventQueueFull:
		m.notice = "message queue is full"
	}

	return nil
}

func (m *model) messageByID(id uint64, role string) *chatMessage {
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].id == id && m.history[i].role == role {
			return &m.history[i]
		}
	}
	return nil
}

func (m *model) assistantMessageForChunk(id uint64) *chatMessage {
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].id != id {
			continue
		}
		switch m.history[i].role {
		case roleAssistant:
			return &m.history[i]
		case roleTool:
			startedAt := time.Now()
			for j := len(m.history) - 1; j >= 0; j-- {
				if m.history[j].id == id && m.history[j].role == roleAssistant && !m.history[j].startedAt.IsZero() {
					startedAt = m.history[j].startedAt
					break
				}
			}
			m.history = append(m.history, chatMessage{
				id:        id,
				role:      roleAssistant,
				status:    statusActive,
				startedAt: startedAt,
			})
			return &m.history[len(m.history)-1]
		}
	}
	return nil
}

func (m *model) messageByToolID(requestID uint64, toolID string) *chatMessage {
	for i := len(m.history) - 1; i >= 0; i-- {
		message := &m.history[i]
		if message.id == requestID && message.role == roleTool && message.toolID == toolID {
			return message
		}
	}
	return nil
}

func (m *model) finishMessage(id uint64, status messageStatus, errorText string) {
	var message *chatMessage
	for i := len(m.history) - 1; i >= 0; i-- {
		candidate := &m.history[i]
		if candidate.id != id || candidate.role != roleAssistant {
			continue
		}
		if message == nil {
			message = candidate
		}
		if candidate.status == statusActive {
			candidate.status = status
		}
	}
	if message == nil {
		return
	}
	message.status = status
	if errorText != "" {
		message.renderedContentValid = false
		if message.content != "" {
			message.content += "\n\n"
		}
		message.content += "[" + errorText + "]"
	}
	m.refreshHistory(true)
}

func (m *model) resize(width, height int) {
	m.windowWidth = width
	m.windowHeight = height
	m.help.SetWidth(width)

	// The input box contributes two rows for its top and bottom borders.
	m.input.SetWidth(atLeastOne(width - 2))

	footerHeight := lipgloss.Height(m.statusView())
	footerHeight += lipgloss.Height(m.completionView())
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
	var workingMessages []string
	for i := 0; i < len(m.history); {
		if isWorkingMessage(m.history[i]) {
			if rendered := renderHistoryMessageWithIndent(&m.history[i], m.viewport.Width(), &m.markdown, "", m.spinner.View()); rendered != "" {
				workingMessages = append(workingMessages, rendered)
			}
			i++
			continue
		}

		if isExploratoryTool(m.history[i]) {
			end := i + 1
			for end < len(m.history) && isExploratoryTool(m.history[end]) {
				end++
			}
			if rendered := renderExploreGroup(m.history[i:end], m.viewport.Width(), &m.markdown); rendered != "" {
				messages = append(messages, rendered)
			}
			i = end
			continue
		}

		if rendered := renderHistoryMessage(&m.history[i], m.viewport.Width(), &m.markdown); rendered != "" {
			messages = append(messages, rendered)
		}
		i++
	}
	messages = append(messages, workingMessages...)

	content := strings.Join(messages, "\n\n")
	m.viewport.SetContent(content)
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func renderHistoryMessage(message *chatMessage, width int, markdown *markdownRenderer) string {
	return renderHistoryMessageWithIndent(message, width, markdown, "", "")
}

func renderHistoryMessageWithIndent(message *chatMessage, width int, markdown *markdownRenderer, indent, spinnerFrame string) string {
	if message.role == roleAssistant && strings.TrimSpace(message.content) == "" && message.status != statusActive {
		return ""
	}

	marker, markerColor := messageMarker(*message)
	contentColor := "252"
	if message.role == roleUser {
		contentColor = userMarkerColor
	}

	markerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(markerColor))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(contentColor))
	contentWidth := atLeastOne(width - lipgloss.Width(indent) - lipgloss.Width(historyContentIndent))
	var lines []string
	if message.role == roleAssistant && strings.TrimSpace(message.content) == "" {
		lines = []string{renderWorkingMessage(*message, spinnerFrame)}
	} else if message.role == roleAssistant {
		if !message.renderedContentValid || message.renderedContentWidth != contentWidth {
			formatted, err := markdown.render(message.content, contentWidth)
			if err != nil {
				formatted = lipgloss.Wrap(message.content, contentWidth, "")
			}
			formatted = strings.TrimSuffix(formatted, "\n")
			message.renderedContent = formatted
			message.renderedContentWidth = contentWidth
			message.renderedContentValid = true
		}
		lines = strings.Split(message.renderedContent, "\n")
	} else {
		lines = strings.Split(lipgloss.Wrap(message.content, contentWidth, ""), "\n")
	}
	rendered := make([]string, len(lines))
	for i, line := range lines {
		prefix := indent + historyContentIndent
		if i == 0 {
			prefix = indent + markerStyle.Render(marker) + " "
		}
		if message.role == roleAssistant {
			rendered[i] = prefix + line
		} else {
			rendered[i] = prefix + contentStyle.Render(line)
		}
	}

	return strings.Join(rendered, "\n")
}

func renderExploreGroup(messages []chatMessage, width int, markdown *markdownRenderer) string {
	if len(messages) == 0 {
		return ""
	}

	marker, markerColor := messageMarker(chatMessage{role: roleAssistant, status: exploreGroupStatus(messages)})
	markerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(markerColor))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	lines := []string{markerStyle.Render(marker) + " " + contentStyle.Render(exploreLabel)}
	for i := range messages {
		if rendered := renderHistoryMessageWithIndent(&messages[i], width, markdown, historyContentIndent, ""); rendered != "" {
			lines = append(lines, rendered)
		}
	}
	return strings.Join(lines, "\n")
}

func exploreGroupStatus(messages []chatMessage) messageStatus {
	status := statusSucceeded
	for _, message := range messages {
		switch message.status {
		case statusActive:
			return statusActive
		case statusFailed:
			status = statusFailed
		case statusCancelled:
			if status != statusFailed {
				status = statusCancelled
			}
		}
	}
	return status
}

func isExploratoryTool(message chatMessage) bool {
	if message.role != roleTool {
		return false
	}

	toolName := message.toolName
	if toolName == "" {
		fields := strings.Fields(message.content)
		if len(fields) == 0 {
			return false
		}
		toolName = fields[0]
	}

	switch toolName {
	case "Read", "Glob", "Grep", "GitStatus", "GitDiff", "GitLog", "GitShow":
		return true
	default:
		return false
	}
}

func workingSeconds(message chatMessage) int {
	if message.startedAt.IsZero() {
		return 0
	}

	elapsed := time.Since(message.startedAt)
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed / time.Second)
}

func renderWorkingMessage(message chatMessage, spinnerFrame string) string {
	spinnerFrame = strings.TrimSpace(spinnerFrame)
	if spinnerFrame != "" {
		spinnerFrame += " "
	}

	workingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(workingColor))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(workingHintColor))
	return workingStyle.Render(spinnerFrame+"Working") + " " + hintStyle.Render(fmt.Sprintf("(%ds · esc to interrupt)", workingSeconds(message)))
}

func isWorkingMessage(message chatMessage) bool {
	return message.role == roleAssistant && message.status == statusActive && strings.TrimSpace(message.content) == ""
}

func messageMarker(message chatMessage) (string, string) {
	if message.role == roleUser {
		return userMarker, userMarkerColor
	}

	switch message.status {
	case statusActive:
		return assistantMarker, activeMarkerColor
	case statusSucceeded:
		return assistantMarker, succeededMarkerColor
	case statusFailed:
		return assistantMarker, failedMarkerColor
	case statusCancelled:
		return assistantMarker, cancelledMarkerColor
	default:
		return assistantMarker, completedMarkerColor
	}
}

func (m model) statusView() string {
	activity := ""
	if m.busy {
		activity = m.spinner.View() + " "
	}

	context := "—"
	if m.contextLimit > 0 {
		context = fmt.Sprintf("%s/%s", formatTokenCount(m.contextUsed), formatTokenCount(m.contextLimit))
	}

	queue := ""
	if m.queued > 0 {
		queue = fmt.Sprintf(" · queued: %d", m.queued)
	}
	notice := ""
	if m.notice != "" {
		notice = " · " + m.notice
	}

	return statusStyle.Render(fmt.Sprintf("%smodel: %s · context: %s%s%s", activity, m.modelName, context, queue, notice))
}

func formatTokenCount(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func (m *model) closeOverlay() {
	m.showSettings = false
	m.showHelp = false
	m.cancelConfirm = false
	m.input.Focus()
	m.resize(m.windowWidth, m.windowHeight)
}

func (m model) View() tea.View {
	if m.cancelConfirm {
		return m.overlayView("Cancel inference?", "Press ctrl+c again to cancel", "Press esc to continue")
	}

	if m.showSettings {
		context := "not connected"
		if m.contextLimit > 0 {
			context = fmt.Sprintf("%d/%d tokens", m.contextUsed, m.contextLimit)
		}
		return m.overlayView("Settings", "Model: "+m.modelName, "Context: "+context, "Press esc to close")
	}

	historyView := m.viewport.View()
	inputView := inputBoxStyle.Render(m.input.View())
	completionView := m.completionView()
	statusView := m.statusView()

	parts := []string{historyView, inputView}
	if completionView != "" {
		parts = append(parts, completionView)
	}
	parts = append(parts, statusView)
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
