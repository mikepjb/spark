package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type ClientConfig struct {
	Endpoint   string
	APIKey     string
	HTTPClient *http.Client
}

type OpenAIClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func NewClient(cfg ClientConfig) (*OpenAIClient, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("LLM endpoint is required")
	}
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return nil, fmt.Errorf("parse LLM endpoint: %w", err)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &OpenAIClient{endpoint: endpoint, apiKey: cfg.APIKey, http: httpClient}, nil
}

func (c *OpenAIClient) Complete(ctx context.Context, request Request) (Stream, error) {
	request.Stream = true
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode completion request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.completionURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create completion request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	response, err := c.http.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send completion request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		message, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		if readErr != nil {
			return nil, fmt.Errorf("LLM returned HTTP %d and response could not be read: %w", response.StatusCode, readErr)
		}
		if len(bytes.TrimSpace(message)) == 0 {
			return nil, fmt.Errorf("LLM returned HTTP %d", response.StatusCode)
		}
		return nil, fmt.Errorf("LLM returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}

	return &stream{body: response.Body, scanner: newScanner(response.Body)}, nil
}

func (c *OpenAIClient) completionURL() string {
	if strings.HasSuffix(c.endpoint, "/v1/chat/completions") {
		return c.endpoint
	}
	if strings.HasSuffix(c.endpoint, "/v1") {
		return c.endpoint + "/chat/completions"
	}
	return c.endpoint + "/v1/chat/completions"
}

type stream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	closed  bool
}

func newScanner(body io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	return scanner
}

func (s *stream) Next() (Delta, error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			return Delta{}, io.EOF
		}

		var response struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &response); err != nil {
			return Delta{}, fmt.Errorf("decode streamed completion: %w", err)
		}
		if len(response.Choices) == 0 || response.Choices[0].Delta.Content == "" {
			continue
		}
		return Delta{Content: response.Choices[0].Delta.Content}, nil
	}

	if err := s.scanner.Err(); err != nil {
		return Delta{}, fmt.Errorf("read streamed completion: %w", err)
	}
	return Delta{}, io.EOF
}

func (s *stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}
