package repl

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/mikepjb/spark/internal/llm"
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
		{Role: "system", Content: "be concise"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "hello world"},
		{Role: "user", Content: "next"},
	}
	if len(request.Messages) != len(want) {
		t.Fatalf("message count = %d, want %d: %+v", len(request.Messages), len(want), request.Messages)
	}
	for i := range want {
		if request.Messages[i] != want[i] {
			t.Fatalf("message %d = %+v, want %+v", i, request.Messages[i], want[i])
		}
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
