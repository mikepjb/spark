package view

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m *model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Close) {
		m.input.Reset()
		m.resize(m.windowWidth, m.windowHeight)
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

	return nil, nil
}
