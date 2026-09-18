package view

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mikepjb/spark/internal/repl"
)

func press(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
	case "pgup":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
	case "pgdown":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	case "ctrl+j":
		return tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl})
	case "ctrl+,":
		return tea.KeyPressMsg(tea.Key{Code: ',', Mod: tea.ModCtrl})
	case "ctrl+c":
		return tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	case "?":
		return tea.KeyPressMsg(tea.Key{Code: '?', Text: "?"})
	default:
		return tea.KeyPressMsg(tea.Key{Code: rune(name[0]), Text: name[:1]})
	}
}

func updateModel(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(model), cmd
}

func TestInitialModelConfiguresMultilineInput(t *testing.T) {
	m := initialModel()
	if len(m.history) != 0 {
		t.Fatalf("expected no placeholder history, got %d messages", len(m.history))
	}

	if !m.input.DynamicHeight {
		t.Fatal("expected dynamic textarea height")
	}
	if m.input.MinHeight != 1 || m.input.MaxHeight != maxInputHeight {
		t.Fatalf("unexpected textarea height bounds: %d-%d", m.input.MinHeight, m.input.MaxHeight)
	}
	if m.input.ShowLineNumbers {
		t.Fatal("expected textarea line numbers to be disabled")
	}
	if !m.input.KeyMap.InsertNewline.Enabled() {
		t.Fatal("expected newline binding to be enabled")
	}
	if got := m.input.KeyMap.InsertNewline.Keys(); len(got) != 1 || got[0] != "ctrl+j" {
		t.Fatalf("unexpected newline keys: %v", got)
	}
}

func TestInputNewlineAndSubmit(t *testing.T) {
	m := initialModel()
	m.resize(60, 20)
	m.input.SetValue("first line")

	m, _ = updateModel(t, m, press("ctrl+j"))
	if got := m.input.Value(); got != "first line\n" {
		t.Fatalf("newline was not inserted, got %q", got)
	}
	if m.input.Height() != 2 {
		t.Fatalf("expected textarea to grow to two rows, got %d", m.input.Height())
	}

	m.input.InsertString("second line")
	m, _ = updateModel(t, m, press("enter"))
	if got := len(m.history); got != 1 {
		t.Fatalf("expected submitted message in history, got %d messages", got)
	}
	if got := m.history[0].content; got != "first line\nsecond line" {
		t.Fatalf("unexpected submitted content: %q", got)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected input to reset, got %q", got)
	}
	if m.input.Height() != 1 {
		t.Fatalf("expected textarea to shrink after submit, got %d", m.input.Height())
	}
}

func TestQuestionMarkIsHelpOnlyForEmptyInput(t *testing.T) {
	m := initialModel()
	m.resize(60, 20)

	m, _ = updateModel(t, m, press("?"))
	if !m.showHelp || m.input.Focused() {
		t.Fatal("expected help to open and input to blur")
	}
	if view := m.View().Content; !strings.Contains(view, "ctrl+,") || !strings.Contains(view, "settings") {
		t.Fatalf("expanded help did not include settings binding: %q", view)
	}

	m, _ = updateModel(t, m, press("esc"))
	if m.showHelp || !m.input.Focused() {
		t.Fatal("expected help to close and input to refocus")
	}

	m.input.SetValue("question")
	m, _ = updateModel(t, m, press("?"))
	if m.showHelp {
		t.Fatal("question mark opened help while composing a message")
	}
	if got := m.input.Value(); got != "question?" {
		t.Fatalf("question mark was not inserted into input: %q", got)
	}
}

func TestSettingsOverlay(t *testing.T) {
	m := initialModel()
	m.resize(60, 20)

	m, _ = updateModel(t, m, press("ctrl+,"))
	if !m.showSettings || m.input.Focused() {
		t.Fatal("expected settings overlay to open and input to blur")
	}
	if view := m.View().Content; !strings.Contains(view, "Settings") {
		t.Fatalf("settings view did not contain its title: %q", view)
	}

	m, _ = updateModel(t, m, press("esc"))
	if m.showSettings || !m.input.Focused() {
		t.Fatal("expected settings overlay to close and input to refocus")
	}
}

func TestQuitBlursRealCursor(t *testing.T) {
	m := initialModel()
	if !m.input.Focused() {
		t.Fatal("expected input to start focused")
	}

	updated, cmd := updateModel(t, m, press("ctrl+c"))
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if updated.input.Focused() || updated.View().Cursor != nil {
		t.Fatal("expected quit to remove the real cursor from the final view")
	}
}

func TestHistoryScrollsWithViewport(t *testing.T) {
	m := initialModel()
	for i := 0; i < 20; i++ {
		m.history = append(m.history, chatMessage{role: roleAssistant, content: "history line"})
	}
	m.resize(40, 8)

	if !m.viewport.AtBottom() {
		t.Fatal("expected history to start at the bottom")
	}
	m, _ = updateModel(t, m, press("pgup"))
	if m.viewport.AtBottom() {
		t.Fatal("expected page up to scroll history")
	}
	m, _ = updateModel(t, m, press("pgdown"))
	if !m.viewport.AtBottom() {
		t.Fatal("expected page down to return to the bottom")
	}
}

func TestHistoryUsesMarkersAndIndentedContent(t *testing.T) {
	m := initialModel()
	m.history = []chatMessage{
		{role: roleAssistant, content: "welcome\ncontinued and a long line that must wrap", status: statusActive},
		{role: roleUser, content: "hello\nagain"},
	}
	m.resize(24, 10)

	content := m.viewport.GetContent()
	if strings.Contains(content, "assistant:") || strings.Contains(content, "you:") {
		t.Fatalf("history still contains role prefixes: %q", content)
	}
	if !strings.Contains(content, assistantMarker) || !strings.Contains(content, userMarker) {
		t.Fatalf("history did not contain role markers: %q", content)
	}
	for _, expected := range []string{"welcome", "continued", "hello", "again"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("history did not contain %q: %q", expected, content)
		}
	}
	if !strings.Contains(content, "\n  ") {
		t.Fatalf("history did not indent continuation content: %q", content)
	}
}

func TestMessageMarkersUseStatusColors(t *testing.T) {
	tests := []struct {
		name  string
		role  string
		state messageStatus
		mark  string
		color string
	}{
		{name: "user", role: roleUser, mark: userMarker, color: userMarkerColor},
		{name: "active", role: roleAssistant, state: statusActive, mark: assistantMarker, color: activeMarkerColor},
		{name: "completed", role: roleAssistant, state: statusCompleted, mark: assistantMarker, color: completedMarkerColor},
		{name: "succeeded", role: roleAssistant, state: statusSucceeded, mark: assistantMarker, color: succeededMarkerColor},
		{name: "failed", role: roleAssistant, state: statusFailed, mark: assistantMarker, color: failedMarkerColor},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mark, color := messageMarker(chatMessage{role: test.role, status: test.state})
			if mark != test.mark || color != test.color {
				t.Fatalf("message marker = %q/%q, want %q/%q", mark, color, test.mark, test.color)
			}
		})
	}
}

func TestHistoryPreservesMessageContent(t *testing.T) {
	m := initialModel()
	m.history = []chatMessage{
		{role: roleAssistant, content: "welcome"},
		{role: roleUser, content: "hello"},
	}
	m.resize(40, 10)

	content := m.viewport.GetContent()
	if !strings.Contains(content, "welcome") || !strings.Contains(content, "hello") {
		t.Fatalf("history content was not rendered: %q", content)
	}
}

func TestStatusShowsBusySpinnerAndContext(t *testing.T) {
	m := initialModel()
	m.busy = true
	m.contextUsed = 128
	m.contextLimit = 4096

	status := m.statusView()
	for _, expected := range []string{"model: not connected", "context: 128/4096", "thinking"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status %q did not contain %q", status, expected)
		}
	}

	if _, cmd := updateModel(t, m, m.spinner.Tick()); cmd == nil {
		t.Fatal("expected busy spinner to schedule its next tick")
	}
}

type fakeBackend struct {
	events    chan repl.Event
	submitID  uint64
	submitErr error
	cancelled int
	closed    bool
}

func (b *fakeBackend) Submit(string) (uint64, error) {
	return b.submitID, b.submitErr
}

func (b *fakeBackend) Events() <-chan repl.Event { return b.events }

func (b *fakeBackend) Cancel() { b.cancelled++ }

func (b *fakeBackend) Close() { b.closed = true }

func TestReplEventsUpdateStreamingHistory(t *testing.T) {
	backend := &fakeBackend{events: make(chan repl.Event), submitID: 1}
	m := newModel(backend, "test-model")
	m.resize(60, 20)

	m, _ = updateModel(t, m, replEventMsg{event: repl.Event{Kind: repl.EventQueued, RequestID: 1, Content: "hello", QueueCount: 1}})
	m, _ = updateModel(t, m, replEventMsg{event: repl.Event{Kind: repl.EventStarted, RequestID: 1}})
	m, _ = updateModel(t, m, replEventMsg{event: repl.Event{Kind: repl.EventChunk, RequestID: 1, Content: "world"}})
	m, _ = updateModel(t, m, replEventMsg{event: repl.Event{Kind: repl.EventCompleted, RequestID: 1}})

	if len(m.history) != 2 {
		t.Fatalf("history length = %d, want 2", len(m.history))
	}
	if m.history[1].content != "world" || m.history[1].status != statusSucceeded {
		t.Fatalf("unexpected assistant message: %+v", m.history[1])
	}
	if m.busy {
		t.Fatal("expected inference to be complete")
	}
}

func TestCancelConfirmationCancelsInference(t *testing.T) {
	backend := &fakeBackend{events: make(chan repl.Event)}
	m := newModel(backend, "test-model")
	m.busy = true

	m, _ = updateModel(t, m, press("ctrl+c"))
	if !m.cancelConfirm || m.input.Focused() {
		t.Fatal("expected cancellation confirmation overlay")
	}
	if backend.cancelled != 0 {
		t.Fatal("inference was cancelled before confirmation")
	}

	m, _ = updateModel(t, m, press("ctrl+c"))
	if m.cancelConfirm || backend.cancelled != 1 {
		t.Fatalf("confirmation did not cancel inference: confirm=%v cancels=%d", m.cancelConfirm, backend.cancelled)
	}
}

func TestFailedStreamPreservesPartialHistory(t *testing.T) {
	m := initialModel()
	m.history = []chatMessage{
		{id: 1, role: roleUser, content: "hello"},
		{id: 1, role: roleAssistant, content: "partial", status: statusActive},
	}
	m.busy = true
	m.resize(60, 20)

	m, _ = updateModel(t, m, replEventMsg{event: repl.Event{Kind: repl.EventFailed, RequestID: 1, Err: errTestFailure}})

	if m.history[1].content != "partial\n\n[test failure]" || m.history[1].status != statusFailed {
		t.Fatalf("partial failure was not preserved: %+v", m.history[1])
	}
	if m.busy {
		t.Fatal("expected failed inference to stop busy state")
	}
}

var errTestFailure = testError("test failure")

type testError string

func (e testError) Error() string { return string(e) }
