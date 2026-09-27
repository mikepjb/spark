package view

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mikepjb/spark/internal/llm"
)

func (m model) saveHistory() error {
	if m.historyFilePath == "" {
		return fmt.Errorf("save history: export path is not configured")
	}

	var apiHistory []llm.Exchange
	if m.backend != nil {
		apiHistory = m.backend.APIHistory()
	}
	content, err := formatDebugLog(m.history, apiHistory)
	if err != nil {
		return fmt.Errorf("save history: %w", err)
	}
	if err := os.WriteFile(m.historyFilePath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("save history: %w", err)
	}
	return nil
}

func formatDebugLog(history []chatMessage, apiHistory []llm.Exchange) (string, error) {
	var builder strings.Builder
	builder.WriteString(formatHistory(history))
	builder.WriteString("\n## API exchanges\n")
	if len(apiHistory) == 0 {
		builder.WriteString("\nNo API exchanges recorded.\n")
		return builder.String(), nil
	}

	for i, exchange := range apiHistory {
		data, err := json.MarshalIndent(exchange, "", "  ")
		if err != nil {
			return "", fmt.Errorf("encode API exchange %d: %w", i+1, err)
		}
		fmt.Fprintf(&builder, "\n### Exchange %d\n\n```json\n%s\n```\n", i+1, data)
	}
	return builder.String(), nil
}

func formatHistory(history []chatMessage) string {
	var builder strings.Builder
	builder.WriteString("# Spark message history\n")

	for _, message := range history {
		if message.role == roleAssistant && strings.TrimSpace(message.content) == "" {
			continue
		}
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
