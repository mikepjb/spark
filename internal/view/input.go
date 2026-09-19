package view

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/mikepjb/spark/internal/commands"
)

// handleKeyPress handles key presses in the 'default' UI state of spark i.e
// when no modals are showing etc.
func (m model) handleKeyPress(msg tea.KeyPressMsg) (model, tea.Cmd, bool) {
	if len(m.completion.Items) > 0 {
		if key.Matches(msg, m.keys.Escape) {
			m.completion = commands.Completion{}
			m.completionIdx = 0
			return m, nil, true
		}
		if key.Matches(msg, m.keys.CompletionUp) {
			m.completionIdx = (m.completionIdx - 1 + len(m.completion.Items)) % len(m.completion.Items)
			return m, nil, true
		}
		if key.Matches(msg, m.keys.CompletionDown) {
			m.completionIdx = (m.completionIdx + 1) % len(m.completion.Items)
			return m, nil, true
		}
		if key.Matches(msg, m.keys.CompletionAccept) || key.Matches(msg, m.keys.Submit) {
			candidate := m.completion.Items[m.completionIdx]
			value, cursor := commands.Apply(m.input.Value(), m.completion.Token, candidate)
			m.setInputValue(value, cursor)
			m.refreshCompletion()
			return m, nil, true
		}
	}

	if key.Matches(msg, m.keys.Settings) {
		m.showSettings = true
		m.input.Blur()
		return m, nil, true
	}

	// Keep '?' available as normal chat input once the user has started typing.
	if key.Matches(msg, m.keys.Help) && strings.TrimSpace(m.input.Value()) == "" {
		m.showHelp = true
		m.help.ShowAll = true
		m.input.Blur()
		m.resize(m.windowWidth, m.windowHeight)
		return m, nil, true
	}

	if key.Matches(msg, m.keys.Submit) {
		message := strings.TrimSpace(m.input.Value())
		if message == "" {
			return m, nil, true
		}

		if m.commands != nil {
			prepared, err := m.commands.Prepare(message)
			if err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			if prepared.Local != "" {
				m.history = append(m.history,
					chatMessage{role: roleUser, content: message},
					chatMessage{role: roleAssistant, content: prepared.Local, status: statusSucceeded},
				)
				if prepared.ModelName != "" {
					m.modelName = prepared.ModelName
				}
			} else if prepared.Submission != nil && m.backend != nil {
				var err error
				if submission, ok := m.backend.(submissionBackend); ok {
					_, err = submission.SubmitSubmission(*prepared.Submission)
				} else {
					_, err = m.backend.Submit(prepared.Submission.Prompt)
				}
				if err != nil {
					m.notice = err.Error()
					return m, nil, true
				}
			} else if m.backend == nil {
				m.history = append(m.history, chatMessage{role: roleUser, content: message})
			}
		} else if m.backend == nil {
			m.history = append(m.history, chatMessage{role: roleUser, content: message})
		} else {
			if _, err := m.backend.Submit(message); err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
		}
		m.input.Reset()
		m.completion = commands.Completion{}
		m.completionIdx = 0
		m.refreshHistory(true)
		return m, nil, true
	}

	if key.Matches(msg, m.keys.SaveHistory) {
		if err := m.saveHistory(); err != nil {
			m.notice = err.Error()
		} else {
			m.notice = "history saved to " + historyExportFilename
		}
		return m, nil, true
	}

	if key.Matches(msg, m.keys.ScrollUp) || key.Matches(msg, m.keys.ScrollDown) {
		m.viewport, _ = m.viewport.Update(msg)
		return m, nil, true
	}

	return m, nil, false
}
