package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mikepjb/spark/internal/llm"
	"github.com/mikepjb/spark/internal/tools"
)

type EventKind string

const (
	EventQueued        EventKind = "queued"
	EventStarted       EventKind = "started"
	EventChunk         EventKind = "chunk"
	EventCompleted     EventKind = "completed"
	EventFailed        EventKind = "failed"
	EventCancelled     EventKind = "cancelled"
	EventContext       EventKind = "context"
	EventToolStarted   EventKind = "tool-started"
	EventToolCompleted EventKind = "tool-completed"
	EventQueueCleared  EventKind = "queue-cleared"
	EventQueueFull     EventKind = "queue-full"
)

type Event struct {
	Kind         EventKind
	RequestID    uint64
	Content      string
	ToolCallID   string
	Failed       bool
	QueueCount   int
	ContextUsed  int
	ContextLimit int
	Err          error
}

type Config struct {
	Model        string
	SystemPrompt string
	QueueLimit   int
	ContextLimit int
	Tools        *tools.Registry
}

type SkillUse struct {
	Name string
	Body string
}

type Submission struct {
	Display string
	Prompt  string
	Skills  []SkillUse
}

var (
	ErrClosed    = errors.New("repl is closed")
	ErrQueueFull = errors.New("repl queue is full")
)

const maxToolRounds = 8

type Coordinator struct {
	client llm.Client
	config Config

	mu           sync.Mutex
	queue        []request
	transcript   []llm.Message
	activeSkills map[string]string
	activeCancel context.CancelFunc
	closed       bool

	wake   chan struct{}
	done   chan struct{}
	events chan Event
	once   sync.Once
	nextID atomic.Uint64
}

type request struct {
	id        uint64
	display   string
	content   string
	skillUses []SkillUse
}

func New(client llm.Client, config Config) *Coordinator {
	if config.QueueLimit < 1 {
		config.QueueLimit = 5
	}
	if config.ContextLimit < 1 {
		config.ContextLimit = 64000
	}

	c := &Coordinator{
		client:       client,
		config:       config,
		activeSkills: make(map[string]string),
		wake:         make(chan struct{}, 1),
		done:         make(chan struct{}),
		events:       make(chan Event, 64),
	}
	go c.run()
	return c
}

func sortedSkillNames(skills map[string]string) []string {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Coordinator) Events() <-chan Event {
	return c.events
}

func (c *Coordinator) Submit(content string) (uint64, error) {
	return c.SubmitSubmission(Submission{Display: content, Prompt: content})
}

func (c *Coordinator) SubmitSubmission(submission Submission) (uint64, error) {
	display := strings.TrimSpace(submission.Display)
	content := strings.TrimSpace(submission.Prompt)
	if display == "" {
		display = content
	}
	if content == "" {
		content = display
	}
	if display == "" {
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
	c.queue = append(c.queue, request{id: id, display: display, content: content, skillUses: submission.Skills})
	queueCount := len(c.queue)
	c.mu.Unlock()
	c.emit(Event{Kind: EventQueued, RequestID: id, Content: display, QueueCount: queueCount})
	c.signal()
	return id, nil
}

func (c *Coordinator) SetModel(client llm.Client, model string) error {
	if client == nil {
		return fmt.Errorf("model client cannot be nil")
	}
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.client = client
	c.config.Model = model
	return nil
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
		messages = append(messages, llm.Message{Role: "system", Content: llm.StringContent(c.config.SystemPrompt)})
	}
	for _, skill := range req.skillUses {
		if _, exists := c.activeSkills[skill.Name]; !exists {
			c.activeSkills[skill.Name] = skill.Body
		}
	}
	for _, name := range sortedSkillNames(c.activeSkills) {
		messages = append(messages, llm.Message{Role: "system", Content: llm.StringContent(c.activeSkills[name])})
	}
	messages = append(messages, c.transcript...)
	messages = append(messages, llm.Message{Role: "user", Content: llm.StringContent(req.content)})
	client := c.client
	model := c.config.Model
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

	var intermediate []llm.Message
	for round := 0; ; round++ {
		stream, err := client.Complete(ctx, llm.Request{
			Model:         model,
			Messages:      messages,
			Stream:        true,
			Tools:         toolDefinitions(c.config.Tools),
			StreamOptions: &llm.StreamOptions{IncludeUsage: true},
		})
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				c.emit(Event{Kind: EventCancelled, RequestID: req.id})
				return
			}
			c.fail(req.id, err, true)
			return
		}

		response, calls, streamErr := c.readStream(ctx, req.id, stream)
		_ = stream.Close()
		if streamErr != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				c.emit(Event{Kind: EventCancelled, RequestID: req.id, Content: response})
				return
			}
			c.fail(req.id, streamErr, true)
			return
		}
		if len(calls) == 0 {
			c.complete(req, response, intermediate)
			return
		}
		if round >= maxToolRounds {
			c.fail(req.id, fmt.Errorf("tool-call limit reached after %d rounds", maxToolRounds), true)
			return
		}

		assistant := llm.Message{Role: "assistant", Content: contentOrNil(response), ToolCalls: assistantToolCalls(calls)}
		messages = append(messages, assistant)
		intermediate = append(intermediate, assistant)
		for _, call := range calls {
			c.emit(Event{Kind: EventToolStarted, RequestID: req.id, ToolCallID: call.ID, Content: call.Name})
			result := tools.Result{Summary: "tool unavailable", Content: "tool execution is unavailable", Failed: true}
			if c.config.Tools != nil {
				result = c.config.Tools.Execute(ctx, call)
			}
			c.emit(Event{Kind: EventToolCompleted, RequestID: req.id, ToolCallID: call.ID, Content: result.Summary, Failed: result.Failed})
			toolMessage := llm.Message{Role: "tool", Content: llm.StringContent(result.Content), ToolCallID: call.ID}
			messages = append(messages, toolMessage)
			intermediate = append(intermediate, toolMessage)
		}
	}
}

func (c *Coordinator) readStream(ctx context.Context, requestID uint64, stream llm.Stream) (string, []llm.ToolCall, error) {
	var response strings.Builder
	fragments := make(map[int]*llm.ToolCallDelta)
	for {
		delta, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if ctx.Err() != nil {
					return response.String(), nil, ctx.Err()
				}
				return response.String(), assembleToolCalls(fragments), nil
			}
			return response.String(), nil, err
		}
		if delta.Usage != nil {
			c.emit(Event{Kind: EventContext, RequestID: requestID, ContextUsed: delta.Usage.TotalTokens, ContextLimit: c.config.ContextLimit})
		}
		if delta.Content != "" {
			response.WriteString(delta.Content)
			c.emit(Event{Kind: EventChunk, RequestID: requestID, Content: delta.Content})
		}
		for _, fragment := range delta.ToolCall {
			call := fragments[fragment.Index]
			if call == nil {
				call = &llm.ToolCallDelta{Index: fragment.Index}
				fragments[fragment.Index] = call
			}
			if fragment.ID != "" {
				call.ID = fragment.ID
			}
			if fragment.Name != "" {
				call.Name = fragment.Name
			}
			call.Arguments += fragment.Arguments
		}
		if err := ctx.Err(); err != nil {
			return response.String(), nil, err
		}
	}
}

func assembleToolCalls(fragments map[int]*llm.ToolCallDelta) []llm.ToolCall {
	indices := make([]int, 0, len(fragments))
	for index := range fragments {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	calls := make([]llm.ToolCall, 0, len(indices))
	for _, index := range indices {
		fragment := fragments[index]
		id := fragment.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", index)
		}
		arguments := strings.TrimSpace(fragment.Arguments)
		if arguments == "" {
			arguments = "{}"
		}
		calls = append(calls, llm.ToolCall{ID: id, Name: fragment.Name, Arguments: json.RawMessage(arguments)})
	}
	return calls
}

func assistantToolCalls(calls []llm.ToolCall) []llm.AssistantToolCall {
	result := make([]llm.AssistantToolCall, 0, len(calls))
	for _, call := range calls {
		result = append(result, llm.AssistantToolCall{
			Type: "function",
			ID:   call.ID,
			Function: llm.AssistantFunction{
				Name:      call.Name,
				Arguments: string(call.Arguments),
			},
		})
	}
	return result
}

func toolDefinitions(registry *tools.Registry) []llm.ToolDefinition {
	if registry == nil {
		return nil
	}
	return registry.Definitions()
}

func (c *Coordinator) complete(req request, response string, intermediate []llm.Message) {
	c.mu.Lock()
	c.transcript = append(c.transcript,
		llm.Message{Role: "user", Content: llm.StringContent(req.content)},
	)
	c.transcript = append(c.transcript, intermediate...)
	c.transcript = append(c.transcript, llm.Message{Role: "assistant", Content: llm.StringContent(response)})
	c.mu.Unlock()
	c.emit(Event{Kind: EventCompleted, RequestID: req.id})
}

func contentOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return llm.StringContent(value)
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
