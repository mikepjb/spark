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
		defer func() { _ = response.Body.Close() }()
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

// DiscoverModel returns the single model advertised by llama.cpp's OpenAI-compatible API.
func (c *OpenAIClient) DiscoverModel(ctx context.Context) (string, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, c.modelsURL(), nil)
	if err != nil {
		return "", fmt.Errorf("create model discovery request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	response, err := c.http.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("request server models: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("model discovery returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode server models: %w", err)
	}
	models := make([]string, 0, len(result.Data))
	for _, model := range result.Data {
		if id := strings.TrimSpace(model.ID); id != "" && model.OwnedBy == "llamacpp" {
			models = append(models, id)
		}
	}
	if len(models) != 1 {
		return "", fmt.Errorf("server returned %d model IDs; expected one", len(models))
	}
	return models[0], nil
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

func (c *OpenAIClient) modelsURL() string {
	if strings.HasSuffix(c.endpoint, "/v1/chat/completions") {
		return strings.TrimSuffix(c.endpoint, "/chat/completions") + "/models"
	}
	if strings.HasSuffix(c.endpoint, "/v1") {
		return c.endpoint + "/models"
	}
	return c.endpoint + "/v1/models"
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
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &response); err != nil {
			return Delta{Raw: payload}, fmt.Errorf("decode streamed completion: %w", err)
		}

		delta := Delta{Model: response.Model, Raw: payload}
		if response.Usage != nil {
			delta.Usage = response.Usage
		}
		if len(response.Choices) == 0 {
			if delta.Usage != nil {
				return delta, nil
			}
			continue
		}
		delta.Content = response.Choices[0].Delta.Content
		delta.ReasoningContent = response.Choices[0].Delta.ReasoningContent
		delta.Done = response.Choices[0].FinishReason != nil
		if response.Choices[0].FinishReason != nil {
			delta.Finish = *response.Choices[0].FinishReason
		}
		for _, toolCall := range response.Choices[0].Delta.ToolCalls {
			delta.ToolCall = append(delta.ToolCall, ToolCallDelta{
				Index:     toolCall.Index,
				ID:        toolCall.ID,
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			})
		}
		if delta.Content == "" && delta.ReasoningContent == "" && len(delta.ToolCall) == 0 && delta.Usage == nil && !delta.Done && delta.Model == "" {
			continue
		}
		return delta, nil
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
