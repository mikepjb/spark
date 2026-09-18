package llm

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIClientStreamsChatCompletion(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}

		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !request.Stream || request.Model != "test-model" || len(request.Messages) != 1 {
			t.Errorf("unexpected request body: %+v", request)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Endpoint: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Complete(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil || first.Content != "hel" {
		t.Fatalf("first delta = %+v, err = %v", first, err)
	}
	second, err := stream.Next()
	if err != nil || second.Content != "lo" {
		t.Fatalf("second delta = %+v, err = %v", second, err)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("stream end error = %v, want io.EOF", err)
	}
}

func TestOpenAIClientSupportsV1Endpoint(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("stream end error = %v, want io.EOF", err)
	}
}

func TestOpenAIClientReturnsHTTPError(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestOpenAIClientReturnsMalformedStreamError(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: not-json\n\n")
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err == nil {
		t.Fatal("expected malformed stream error")
	}
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}
