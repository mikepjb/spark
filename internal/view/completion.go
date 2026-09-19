package view

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mikepjb/spark/internal/commands"
)

var (
	completionStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	completionSelectedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	completionDescriptionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

const maxCompletionItems = 6

func (m *model) refreshCompletion() {
	if m.commands == nil {
		m.completion = commands.Completion{}
		m.completionIdx = 0
		return
	}
	offset := m.cursorOffset()
	m.completion = m.commands.Candidates(m.input.Value(), offset)
	if len(m.completion.Items) == 0 {
		m.completionIdx = 0
		return
	}
	if m.completionIdx >= len(m.completion.Items) {
		m.completionIdx = len(m.completion.Items) - 1
	}
}

func (m model) cursorOffset() int {
	value := m.input.Value()
	line := m.input.Line()
	column := m.input.Column()
	runes := []rune(value)
	currentLine := 0
	offset := 0
	for offset < len(runes) && currentLine < line {
		if runes[offset] == '\n' {
			currentLine++
		}
		offset++
	}
	lineStart := offset
	for offset < len(runes) && runes[offset] != '\n' {
		offset++
	}
	lineLength := offset - lineStart
	if column > lineLength {
		column = lineLength
	}
	return lineStart + column
}

func (m *model) setInputValue(value string, cursorOffset int) {
	m.input.SetValue(value)
	if cursorOffset < 0 {
		cursorOffset = 0
	}
	runes := []rune(value)
	if cursorOffset > len(runes) {
		cursorOffset = len(runes)
	}
	line := 0
	column := 0
	for i := 0; i < cursorOffset; i++ {
		if runes[i] == '\n' {
			line++
			column = 0
		} else {
			column++
		}
	}
	for i := 0; i < line; i++ {
		m.input.CursorDown()
	}
	m.input.SetCursorColumn(column)
}

func (m model) completionView() string {
	if len(m.completion.Items) == 0 {
		return ""
	}
	items := m.completion.Items
	start := 0
	if m.completionIdx >= maxCompletionItems {
		start = m.completionIdx - maxCompletionItems + 1
	}
	end := start + maxCompletionItems
	if end > len(items) {
		end = len(items)
	}
	items = items[start:end]
	lines := make([]string, 0, len(items))
	for i, item := range items {
		itemIndex := start + i
		marker := "  "
		style := completionStyle
		if itemIndex == m.completionIdx {
			marker = "› "
			style = completionSelectedStyle
		}
		line := style.Render(marker + item.Label)
		if item.Description != "" {
			line += " " + completionDescriptionStyle.Render(item.Description)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
