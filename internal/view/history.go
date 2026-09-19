package view

import (
	"fmt"
	"os"
	"strings"
)

func (m model) saveHistory() error {
	if m.historyFilePath == "" {
		return fmt.Errorf("save history: export path is not configured")
	}

	if err := os.WriteFile(m.historyFilePath, []byte(formatHistory(m.history)), 0o600); err != nil {
		return fmt.Errorf("save history: %w", err)
	}
	return nil
}

func formatHistory(history []chatMessage) string {
	var builder strings.Builder
	builder.WriteString("# Spark message history\n")

	for _, message := range history {
		builder.WriteString("\n## ")
		builder.WriteString(historyRoleLabel(message.role))
		builder.WriteString("\n\n")
		builder.WriteString(message.content)
		if !strings.HasSuffix(message.content, "\n") {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}

func historyRoleLabel(role string) string {
	switch role {
	case roleUser:
		return "User"
	case roleAssistant:
		return "Assistant"
	case roleTool:
		return "Tool"
	default:
		return role
	}
}
