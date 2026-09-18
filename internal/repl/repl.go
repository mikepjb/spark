package repl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mikepjb/spark/internal/llm"
)

type EventKind string

const (
	EventQueued       EventKind = "queued"
	EventStarted      EventKind = "started"
	EventChunk        EventKind = "chunk"
	EventCompleted    EventKind = "completed"
	EventFailed       EventKind = "failed"
	EventCancelled    EventKind = "cancelled"
	EventQueueCleared EventKind = "queue-cleared"
	EventQueueFull    EventKind = "queue-full"
)

type Event struct {
	Kind       EventKind
	RequestID  uint64
	Content    string
	QueueCount int
	Err        error
}

type Config struct {
	Model        string
	SystemPrompt string
	QueueLimit   int
}

var (
	ErrClosed    = errors.New("repl is closed")
	ErrQueueFull = errors.New("repl queue is full")
)

type Coordinator struct {
	client llm.Client
	config Config

	mu           sync.Mutex
	queue        []request
	transcript   []llm.Message
	activeCancel context.CancelFunc
	closed       bool

	wake   chan struct{}
	done   chan struct{}
	events chan Event
	once   sync.Once
	nextID atomic.Uint64
}

type request struct {
	id      uint64
	content string
}

func New(client llm.Client, config Config) *Coordinator {
	if config.QueueLimit < 1 {
		config.QueueLimit = 5
	}

	c := &Coordinator{
		client: client,
		config: config,
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
		events: make(chan Event, 64),
	}
	go c.run()
	return c
}

func (c *Coordinator) Events() <-chan Event {
	return c.events
}

func (c *Coordinator) Submit(content string) (uint64, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, fmt.Errorf("message cannot be empty")
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, ErrClosed
	}
	if len(c.queue) >= c.config.QueueLimit {
		queueCount := len(c.queue)
		c.mu.Unlock()
		c.emit(Event{Kind: EventQueueFull, QueueCount: queueCount, Err: ErrQueueFull})
		return 0, ErrQueueFull
	}

	id := c.nextID.Add(1)
	c.queue = append(c.queue, request{id: id, content: content})
	queueCount := len(c.queue)
	c.mu.Unlock()
	c.emit(Event{Kind: EventQueued, RequestID: id, Content: content, QueueCount: queueCount})
	c.signal()
	return id, nil
}

func (c *Coordinator) Cancel() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if c.activeCancel != nil {
		c.activeCancel()
	}
	cleared := len(c.queue) > 0
	c.queue = nil
	c.mu.Unlock()
	if cleared {
		c.emit(Event{Kind: EventQueueCleared})
	}
}

func (c *Coordinator) Close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		if c.activeCancel != nil {
			c.activeCancel()
		}
		c.queue = nil
		close(c.done)
		c.mu.Unlock()
		c.signal()
	})
}

func (c *Coordinator) run() {
	defer close(c.events)

	for {
		req, ctx, ok := c.next()
		if !ok {
			return
		}
		c.process(req, ctx)
	}
}

func (c *Coordinator) next() (request, context.Context, bool) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			req := c.queue[0]
			c.queue = c.queue[1:]
			ctx, cancel := context.WithCancel(context.Background())
			c.activeCancel = cancel
			c.mu.Unlock()
			return req, ctx, true
		}
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return request{}, nil, false
		}

		select {
		case <-c.wake:
		case <-c.done:
			return request{}, nil, false
		}
	}
}

func (c *Coordinator) process(req request, ctx context.Context) {
	c.mu.Lock()
	messages := make([]llm.Message, 0, len(c.transcript)+2)
	if c.config.SystemPrompt != "" {
		messages = append(messages, llm.Message{Role: "system", Content: c.config.SystemPrompt})
	}
	messages = append(messages, c.transcript...)
	messages = append(messages, llm.Message{Role: "user", Content: req.content})
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.activeCancel != nil {
			c.activeCancel()
		}
		c.activeCancel = nil
		c.mu.Unlock()
	}()

	c.emit(Event{Kind: EventStarted, RequestID: req.id})
	stream, err := c.client.Complete(ctx, llm.Request{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			c.emit(Event{Kind: EventCancelled, RequestID: req.id})
			return
		}
		c.fail(req.id, err, true)
		return
	}
	defer stream.Close()

	var response strings.Builder
	for {
		delta, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if errors.Is(ctx.Err(), context.Canceled) {
					c.emit(Event{Kind: EventCancelled, RequestID: req.id, Content: response.String()})
					return
				}
				c.complete(req, response.String())
				return
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				c.emit(Event{Kind: EventCancelled, RequestID: req.id, Content: response.String()})
				return
			}
			c.fail(req.id, err, true)
			return
		}
		if delta.Content == "" {
			continue
		}
		response.WriteString(delta.Content)
		c.emit(Event{Kind: EventChunk, RequestID: req.id, Content: delta.Content})
	}
}

func (c *Coordinator) complete(req request, response string) {
	c.mu.Lock()
	c.transcript = append(c.transcript,
		llm.Message{Role: "user", Content: req.content},
		llm.Message{Role: "assistant", Content: response},
	)
	c.mu.Unlock()
	c.emit(Event{Kind: EventCompleted, RequestID: req.id})
}

func (c *Coordinator) fail(id uint64, err error, clearQueue bool) {
	if clearQueue {
		c.mu.Lock()
		c.queue = nil
		c.mu.Unlock()
		c.emit(Event{Kind: EventQueueCleared})
	}
	c.emit(Event{Kind: EventFailed, RequestID: id, Err: err})
}

func (c *Coordinator) emit(event Event) {
	select {
	case c.events <- event:
	case <-c.done:
	}
}

func (c *Coordinator) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}
