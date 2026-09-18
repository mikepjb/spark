package llm

import (
	"context"
	"encoding/json"
	"fmt"
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
		Messages: []Message{{Role: "user", Content: StringContent("hello")}},
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

func TestOpenAIClientStreamsToolCallsAndUsage(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != "Read" {
			t.Fatalf("tools = %+v", request.Tools)
		}
		if request.StreamOptions == nil || !request.StreamOptions.IncludeUsage {
			t.Fatalf("stream options = %+v", request.StreamOptions)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		first := map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0,
								"id":    "call_1",
								"function": map[string]string{
									"name":      "Read",
									"arguments": `{"filePath":"notes`,
								},
							},
						},
					},
				},
			},
		}
		second := map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0,
								"function": map[string]string{
									"arguments": `.txt"}`,
								},
							},
						},
					},
				},
			},
		}
		firstJSON, _ := json.Marshal(first)
		secondJSON, _ := json.Marshal(second)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", firstJSON)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", secondJSON)
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":5,\"total_tokens\":17}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Complete(context.Background(), Request{
		Tools: []ToolDefinition{{
			Type:     "function",
			Function: FunctionDefinition{Name: "Read", Parameters: json.RawMessage(`{"type":"object"}`)},
		}},
		StreamOptions: &StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil || len(first.ToolCall) != 1 || first.ToolCall[0].Arguments == "" {
		t.Fatalf("first delta = %+v, err = %v", first, err)
	}
	second, err := stream.Next()
	if err != nil || len(second.ToolCall) != 1 || second.ToolCall[0].Arguments != ".txt\"}" {
		t.Fatalf("second delta = %+v, err = %v", second, err)
	}
	usage, err := stream.Next()
	if err != nil || usage.Usage == nil || usage.Usage.TotalTokens != 17 {
		t.Fatalf("usage delta = %+v, err = %v", usage, err)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("stream end error = %v, want io.EOF", err)
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
