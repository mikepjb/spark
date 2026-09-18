package view

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// handleKeyPress handles key presses in the 'default' UI state of spark i.e
// when no modals are showing etc.
func (m model) handleKeyPress(msg tea.KeyPressMsg) (model, tea.Cmd, bool) {
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

		if m.backend == nil {
			m.history = append(m.history, chatMessage{role: roleUser, content: message})
		} else {
			if _, err := m.backend.Submit(message); err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
		}
		m.input.Reset()
		m.refreshHistory(true)
		return m, nil, true
	}

	if key.Matches(msg, m.keys.ScrollUp) || key.Matches(msg, m.keys.ScrollDown) {
		m.viewport, _ = m.viewport.Update(msg)
		return m, nil, true
	}

	return m, nil, false
}
