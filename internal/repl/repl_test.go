package repl

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikepjb/spark/internal/llm"
	"github.com/mikepjb/spark/internal/tools"
)

type fakeClient struct {
	mu       sync.Mutex
	requests []llm.Request
	streams  chan llm.Stream
}

func newFakeClient() *fakeClient {
	return &fakeClient{streams: make(chan llm.Stream, 10)}
}

func (c *fakeClient) Complete(ctx context.Context, request llm.Request) (llm.Stream, error) {
	c.mu.Lock()
	c.requests = append(c.requests, request)
	c.mu.Unlock()
	stream := <-c.streams
	if blocking, ok := stream.(*blockingStream); ok {
		blocking.ctx = ctx
	}
	return stream, nil
}

func (c *fakeClient) request(index int) llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[index]
}

type fakeStream struct {
	deltas []string
	index  int
}

func (s *fakeStream) Next() (llm.Delta, error) {
	if s.index == len(s.deltas) {
		return llm.Delta{}, io.EOF
	}
	delta := llm.Delta{Content: s.deltas[s.index]}
	s.index++
	return delta, nil
}

func (s *fakeStream) Close() error { return nil }

type deltaStream struct {
	deltas []llm.Delta
	index  int
}

func (s *deltaStream) Next() (llm.Delta, error) {
	if s.index == len(s.deltas) {
		return llm.Delta{}, io.EOF
	}
	delta := s.deltas[s.index]
	s.index++
	return delta, nil
}

func (s *deltaStream) Close() error { return nil }

type errorStream struct {
	err error
}

func (s *errorStream) Next() (llm.Delta, error) { return llm.Delta{}, s.err }

func (s *errorStream) Close() error { return nil }

type blockingStream struct {
	started chan struct{}
	ctx     context.Context
}

func (s *blockingStream) Next() (llm.Delta, error) {
	select {
	case <-s.ctx.Done():
		return llm.Delta{}, s.ctx.Err()
	case <-s.started:
		return llm.Delta{}, io.EOF
	case <-time.After(5 * time.Second):
		return llm.Delta{}, errors.New("test stream timeout")
	}
}

func (s *blockingStream) Close() error { return nil }

func TestCoordinatorStreamsAndBuildsConversationHistory(t *testing.T) {
	client := newFakeClient()
	coordinator := New(client, Config{Model: "test-model", SystemPrompt: "be concise", QueueLimit: 5})
	defer coordinator.Close()

	client.streams <- &fakeStream{deltas: []string{"hello", " world"}}
	firstID, err := coordinator.Submit("first")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventQueued, firstID)
	waitForEvent(t, coordinator.Events(), EventStarted, firstID)
	waitForEvent(t, coordinator.Events(), EventChunk, firstID)
	waitForEvent(t, coordinator.Events(), EventChunk, firstID)
	waitForEvent(t, coordinator.Events(), EventCompleted, firstID)

	client.streams <- &fakeStream{deltas: []string{"second"}}
	secondID, err := coordinator.Submit("next")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventQueued, secondID)
	waitForEvent(t, coordinator.Events(), EventStarted, secondID)
	waitForEvent(t, coordinator.Events(), EventChunk, secondID)
	waitForEvent(t, coordinator.Events(), EventCompleted, secondID)

	request := client.request(1)
	want := []llm.Message{
		{Role: "system", Content: llm.StringContent("be concise")},
		{Role: "user", Content: llm.StringContent("first")},
		{Role: "assistant", Content: llm.StringContent("hello world")},
		{Role: "user", Content: llm.StringContent("next")},
	}
	if len(request.Messages) != len(want) {
		t.Fatalf("message count = %d, want %d: %+v", len(request.Messages), len(want), request.Messages)
	}
	for i := range want {
		if !reflect.DeepEqual(request.Messages[i], want[i]) {
			t.Fatalf("message %d = %+v, want %+v", i, request.Messages[i], want[i])
		}
	}
}

func TestCoordinatorAddsRuntimeEnvironmentToSystemPrompt(t *testing.T) {
	client := newFakeClient()
	client.streams <- &fakeStream{deltas: []string{"done"}}
	coordinator := New(client, Config{
		Model:        "test-model",
		SystemPrompt: "be concise",
		Environment: Environment{
			WorkingDirectory: "/workspace/project",
			IsGitRepository:  true,
			Platform:         "linux",
		},
		Now: func() time.Time {
			return time.Date(2026, time.September, 20, 14, 35, 0, 0, time.FixedZone("BST", 3600))
		},
	})
	defer coordinator.Close()

	id, err := coordinator.Submit("inspect this")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventCompleted, id)

	request := client.request(0)
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[0].Content == nil {
		t.Fatalf("request messages = %+v", request.Messages)
	}
	content := *request.Messages[0].Content
	for _, expected := range []string{
		"be concise",
		"<env>",
		"Working directory: /workspace/project",
		"Is directory a git repo: yes",
		"Platform: linux",
		"Today's date: Sun Sep 20 2026",
		"Local time: 14:35 BST",
		"Timezone: BST (BST +0100)",
		"</env>",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("system prompt missing %q: %q", expected, content)
		}
	}
}

func TestCoordinatorActivatesSkillsForOneRequest(t *testing.T) {
	client := newFakeClient()
	client.streams <- &fakeStream{deltas: []string{"done"}}
	client.streams <- &fakeStream{deltas: []string{"again"}}
	coordinator := New(client, Config{Model: "test-model", SystemPrompt: "base"})
	defer coordinator.Close()

	firstID, err := coordinator.SubmitSubmission(Submission{
		Display: "/analyse inspect",
		Prompt:  "inspect",
		Skills: []SkillUse{
			{Name: "analyse", Body: "Use evidence."},
			{Name: "analyse", Body: "Use evidence again."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventCompleted, firstID)
	firstRequest := client.request(0)
	if len(firstRequest.Messages) != 2 || firstRequest.Messages[1].Role != "user" || firstRequest.Messages[1].Content == nil {
		t.Fatalf("first request messages = %+v", firstRequest.Messages)
	}
	firstContent := *firstRequest.Messages[1].Content
	if !strings.Contains(firstContent, "Use evidence.") || strings.Contains(firstContent, "Use evidence again.") || !strings.Contains(firstContent, "inspect") {
		t.Fatalf("first request user content = %q", firstContent)
	}
	if strings.Contains(*firstRequest.Messages[0].Content, "Use evidence.") {
		t.Fatalf("skill content was injected into the system message: %+v", firstRequest.Messages[0])
	}

	secondID, err := coordinator.Submit("next")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventCompleted, secondID)

	request := client.request(1)
	if len(request.Messages) != 4 {
		t.Fatalf("second request messages = %+v", request.Messages)
	}
	for _, message := range request.Messages {
		if message.Content != nil && strings.Contains(*message.Content, "Use evidence.") {
			t.Fatalf("skill body persisted into second request: %+v", request.Messages)
		}
	}
}

func TestCoordinatorCanSwitchModelsForNewRequests(t *testing.T) {
	first := newFakeClient()
	second := newFakeClient()
	first.streams <- &fakeStream{deltas: []string{"first"}}
	second.streams <- &fakeStream{deltas: []string{"second"}}
	coordinator := New(first, Config{Model: "old-model"})
	defer coordinator.Close()

	firstID, err := coordinator.Submit("one")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventCompleted, firstID)
	if err := coordinator.SetModel(second, "new-model"); err != nil {
		t.Fatal(err)
	}
	secondID, err := coordinator.Submit("two")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventCompleted, secondID)
	if request := second.request(0); request.Model != "new-model" {
		t.Fatalf("new request model = %q", request.Model)
	}
}

func TestCoordinatorBoundsAndClearsQueueOnCancellation(t *testing.T) {
	client := newFakeClient()
	coordinator := New(client, Config{QueueLimit: 5})
	defer coordinator.Close()

	blocking := &blockingStream{started: make(chan struct{})}
	client.streams <- blocking
	activeID, err := coordinator.Submit("active")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventQueued, activeID)
	waitForEvent(t, coordinator.Events(), EventStarted, activeID)

	for i := 0; i < 5; i++ {
		if _, err := coordinator.Submit("queued"); err != nil {
			t.Fatalf("queue request %d: %v", i, err)
		}
	}
	if _, err := coordinator.Submit("overflow"); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("overflow error = %v, want ErrQueueFull", err)
	}

	coordinator.Cancel()
	waitForEvent(t, coordinator.Events(), EventQueueFull, 0)
	waitForEvent(t, coordinator.Events(), EventQueueCleared, 0)
	waitForEvent(t, coordinator.Events(), EventCancelled, activeID)

	select {
	case event := <-coordinator.Events():
		if event.Kind == EventStarted {
			t.Fatalf("queued request started after cancellation: %+v", event)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCoordinatorExecutesToolCallsAndContinuesConversation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("tool result\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := tools.New(root)
	if err != nil {
		t.Fatal(err)
	}

	client := newFakeClient()
	client.streams <- &deltaStream{deltas: []llm.Delta{
		{ToolCall: []llm.ToolCallDelta{{Index: 0, ID: "call_1", Name: "Read", Arguments: `{"filePath":"notes.txt"}`}}},
		{Done: true},
		{Usage: &llm.Usage{TotalTokens: 12}},
	}}
	client.streams <- &deltaStream{deltas: []llm.Delta{
		{Content: "finished"},
		{Done: true},
		{Usage: &llm.Usage{TotalTokens: 20}},
	}}

	coordinator := New(client, Config{Model: "test-model", SystemPrompt: "be concise", Tools: registry})
	defer coordinator.Close()
	id, err := coordinator.Submit("inspect notes")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventQueued, id)
	waitForEvent(t, coordinator.Events(), EventStarted, id)
	contextEvent := waitForEvent(t, coordinator.Events(), EventContext, id)
	if contextEvent.ContextUsed != 12 || contextEvent.ContextLimit != 64000 {
		t.Fatalf("first context event = %+v", contextEvent)
	}
	toolStarted := waitForEvent(t, coordinator.Events(), EventToolStarted, id)
	if toolStarted.Content != "Read" || toolStarted.ToolCallID != "call_1" {
		t.Fatalf("tool started event = %+v", toolStarted)
	}
	toolCompleted := waitForEvent(t, coordinator.Events(), EventToolCompleted, id)
	if toolCompleted.Content != "Read notes.txt (lines 1-1 of 1)" || toolCompleted.Failed {
		t.Fatalf("tool completed event = %+v", toolCompleted)
	}
	waitForEvent(t, coordinator.Events(), EventChunk, id)
	secondContext := waitForEvent(t, coordinator.Events(), EventContext, id)
	if secondContext.ContextUsed != 20 {
		t.Fatalf("second context event = %+v", secondContext)
	}
	waitForEvent(t, coordinator.Events(), EventCompleted, id)

	request := client.request(1)
	if len(request.Tools) != 7 {
		t.Fatalf("tool definition count = %d, want 7", len(request.Tools))
	}
	firstRequest := client.request(0)
	if firstRequest.Messages[0].Content == nil || !strings.Contains(*firstRequest.Messages[0].Content, "Tool calls remaining for this request: 8") {
		t.Fatalf("initial tool budget prompt = %+v", firstRequest.Messages)
	}
	if len(request.Messages) != 4 {
		t.Fatalf("second request messages = %+v", request.Messages)
	}
	if request.Messages[2].Role != "assistant" || len(request.Messages[2].ToolCalls) != 1 {
		t.Fatalf("assistant tool message = %+v", request.Messages[2])
	}
	if request.Messages[3].Role != "tool" || request.Messages[3].ToolCallID != "call_1" || request.Messages[3].Content == nil || *request.Messages[3].Content != "tool result" {
		t.Fatalf("tool result message = %+v", request.Messages[3])
	}

	apiHistory := coordinator.APIHistory()
	if len(apiHistory) != 2 {
		t.Fatalf("API history length = %d, want 2", len(apiHistory))
	}
	if len(apiHistory[1].Messages) != 4 || apiHistory[1].Messages[2].ToolCalls[0].Function.Arguments != `{"filePath":"notes.txt"}` {
		t.Fatalf("second API request = %+v", apiHistory[1])
	}
}

func TestCoordinatorFinalizesAfterToolCallLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("tool result\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := tools.New(root)
	if err != nil {
		t.Fatal(err)
	}

	toolCall := llm.ToolCallDelta{Index: 0, ID: "call_1", Name: "Read", Arguments: `{"filePath":"notes.txt"}`}
	client := newFakeClient()
	client.streams <- &deltaStream{deltas: []llm.Delta{{ToolCall: []llm.ToolCallDelta{toolCall}}}}
	client.streams <- &deltaStream{deltas: []llm.Delta{{Content: "final answer"}}}

	coordinator := New(client, Config{Model: "test-model", Tools: registry, ToolCallLimit: 1})
	defer coordinator.Close()
	id, err := coordinator.Submit("inspect notes")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventQueued, id)
	waitForEvent(t, coordinator.Events(), EventStarted, id)
	waitForEvent(t, coordinator.Events(), EventToolStarted, id)
	waitForEvent(t, coordinator.Events(), EventToolCompleted, id)
	waitForEvent(t, coordinator.Events(), EventChunk, id)
	waitForEvent(t, coordinator.Events(), EventCompleted, id)

	select {
	case event := <-coordinator.Events():
		if event.Kind == EventToolStarted {
			t.Fatalf("tool call at limit was executed: %+v", event)
		}
	case <-time.After(100 * time.Millisecond):
	}

	finalRequest := client.request(1)
	if len(finalRequest.Tools) != 0 {
		t.Fatalf("final request exposed tools: %+v", finalRequest.Tools)
	}
	if len(finalRequest.Messages) != 4 || finalRequest.Messages[3].Role != "user" || finalRequest.Messages[3].Content == nil || !strings.Contains(*finalRequest.Messages[3].Content, "tool-call limit") {
		t.Fatalf("final request messages = %+v", finalRequest.Messages)
	}
	if history := coordinator.APIHistory(); len(history) != 2 {
		t.Fatalf("API history length = %d, want 2", len(history))
	}
}

func TestCoordinatorTruncatesToolBatchAtRemainingCallLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first.txt", "second.txt", "not-executed.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := tools.New(root)
	if err != nil {
		t.Fatal(err)
	}

	client := newFakeClient()
	client.streams <- &deltaStream{deltas: []llm.Delta{{ToolCall: []llm.ToolCallDelta{
		{Index: 0, ID: "call_1", Name: "Read", Arguments: `{"filePath":"first.txt"}`},
		{Index: 1, ID: "call_2", Name: "Read", Arguments: `{"filePath":"second.txt"}`},
		{Index: 2, ID: "call_3", Name: "Read", Arguments: `{"filePath":"not-executed.txt"}`},
	}}}}
	client.streams <- &deltaStream{deltas: []llm.Delta{{Content: "final answer"}}}

	coordinator := New(client, Config{Model: "test-model", Tools: registry, ToolCallLimit: 2})
	defer coordinator.Close()
	id, err := coordinator.Submit("inspect the files")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventQueued, id)
	waitForEvent(t, coordinator.Events(), EventStarted, id)
	waitForEvent(t, coordinator.Events(), EventToolStarted, id)
	waitForEvent(t, coordinator.Events(), EventToolCompleted, id)
	waitForEvent(t, coordinator.Events(), EventToolStarted, id)
	waitForEvent(t, coordinator.Events(), EventToolCompleted, id)
	waitForEvent(t, coordinator.Events(), EventCompleted, id)

	if request := client.request(1); len(request.Tools) != 0 {
		t.Fatalf("final request exposed tools: %+v", request.Tools)
	}
	request := client.request(1)
	if len(request.Messages) != 5 {
		t.Fatalf("final request messages = %+v", request.Messages)
	}
	if len(request.Messages[1].ToolCalls) != 2 {
		t.Fatalf("assistant tool calls = %+v", request.Messages[1].ToolCalls)
	}
	if request.Messages[1].ToolCalls[0].ID != "call_1" || request.Messages[1].ToolCalls[1].ID != "call_2" {
		t.Fatalf("assistant tool calls = %+v", request.Messages[1].ToolCalls)
	}
	if request.Messages[2].ToolCallID != "call_1" || request.Messages[3].ToolCallID != "call_2" {
		t.Fatalf("tool results = %+v", request.Messages[2:4])
	}
}

func TestCoordinatorAddsUserAgentPromptOutsideSystemMessage(t *testing.T) {
	client := newFakeClient()
	client.streams <- &fakeStream{deltas: []string{"done"}}
	coordinator := New(client, Config{
		Model:        "test-model",
		SystemPrompt: "internal steering",
		AgentPrompt:  "personal guidance",
	})
	defer coordinator.Close()

	id, err := coordinator.Submit("inspect this")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventCompleted, id)

	request := client.request(0)
	if len(request.Messages) != 2 {
		t.Fatalf("request messages = %+v", request.Messages)
	}
	if request.Messages[0].Content == nil || *request.Messages[0].Content != "internal steering" {
		t.Fatalf("system message = %+v", request.Messages[0])
	}
	if request.Messages[1].Content == nil || !strings.Contains(*request.Messages[1].Content, "personal guidance") || !strings.Contains(*request.Messages[1].Content, "inspect this") {
		t.Fatalf("user message = %+v", request.Messages[1])
	}
}

func TestCoordinatorRequestTimeoutFailsBlockedStream(t *testing.T) {
	client := newFakeClient()
	client.streams <- &blockingStream{started: make(chan struct{})}
	coordinator := New(client, Config{Model: "test-model", RequestTimeout: 20 * time.Millisecond})
	defer coordinator.Close()

	id, err := coordinator.Submit("inspect this")
	if err != nil {
		t.Fatal(err)
	}
	event := waitForEvent(t, coordinator.Events(), EventFailed, id)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "deadline exceeded") {
		t.Fatalf("timeout error = %v", event.Err)
	}
}

func TestCoordinatorRecordsAPIRequestBeforeStreamFailure(t *testing.T) {
	client := newFakeClient()
	client.streams <- &errorStream{err: errors.New("inference failed")}
	coordinator := New(client, Config{Model: "test-model"})
	defer coordinator.Close()

	id, err := coordinator.Submit("inspect this")
	if err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, coordinator.Events(), EventFailed, id)

	history := coordinator.APIHistory()
	if len(history) != 1 || len(history[0].Messages) != 1 || history[0].Messages[0].Content == nil || *history[0].Messages[0].Content != "inspect this" {
		t.Fatalf("API history = %+v", history)
	}
}

func waitForEvent(t *testing.T, events <-chan Event, kind EventKind, requestID uint64) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind == kind && (requestID == 0 || event.RequestID == requestID) {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", kind)
		}
	}
}
