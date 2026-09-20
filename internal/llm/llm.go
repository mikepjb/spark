package llm

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role       string              `json:"role"`
	Content    *string             `json:"content"`
	ToolCalls  []AssistantToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

func StringContent(value string) *string {
	return &value
}

type Request struct {
	Model         string           `json:"model"`
	Messages      []Message        `json:"messages"`
	Stream        bool             `json:"stream"`
	Tools         []ToolDefinition `json:"tools,omitempty"`
	StreamOptions *StreamOptions   `json:"stream_options,omitempty"`
}

type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type Delta struct {
	Content  string
	ToolCall []ToolCallDelta
	Usage    *Usage
	Done     bool
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type AssistantToolCall struct {
	Type     string            `json:"type"`
	ID       string            `json:"id"`
	Function AssistantFunction `json:"function"`
}

type AssistantFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Stream interface {
	Next() (Delta, error)
	Close() error
}

type Client interface {
	Complete(context.Context, Request) (Stream, error)
}
