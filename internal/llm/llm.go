package llm

import (
	"context"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Delta struct {
	Content string
}

type Stream interface {
	Next() (Delta, error)
	Close() error
}

type Client interface {
	Complete(context.Context, Request) (Stream, error)
}
