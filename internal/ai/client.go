// Package ai implements an OpenAI-compatible streaming chat client. It works
// against OpenAI, DeepSeek, vLLM, Ollama, or any provider speaking the same
// /chat/completions SSE protocol.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jettapindika/zoeyDCBot/internal/memory"
)

// Message is one chat message in OpenAI format.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Config drives the client.
type Config struct {
	BaseURL    string // e.g. https://api.openai.com/v1
	APIKey     string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
	MaxRetries int
}

// Client talks to an OpenAI-compatible endpoint.
type Client struct {
	cfg Config
}

// New builds a client. If HTTPClient is nil a 30s-timeout client is used.
func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	return &Client{cfg: cfg}
}

// ErrStream is wrapped by any failure while consuming the SSE stream.
var ErrStream = errors.New("ai stream")

// Chunk is one delta from a streaming completion.
type Chunk struct {
	Content string
	// Reasoning is the thinking/reasoning token from reasoning models
	// (e.g. DeepSeek R1, o1). It is streamed before Content and should be
	// shown as a "thinking…" indicator, not as the final answer.
	Reasoning string
	// Finish is true on the final chunk (finish_reason="stop").
	Finish bool
	// Latency is the time from request start to the first content token.
	Latency time.Duration
}

// StreamChat streams a completion. history is the rolling context (oldest
// first). It calls onChunk for every delta; the first call has Latency set.
// The returned error is nil on a clean [DONE].
func (c *Client) StreamChat(ctx context.Context, system string, history []memory.Turn, onChunk func(Chunk)) error {
	msgs := []Message{{Role: "system", Content: system}}
	for _, t := range history {
		msgs = append(msgs, Message{Role: t.Role, Content: t.Content})
	}

	body, err := json.Marshal(map[string]any{
		"model":       c.cfg.Model,
		"messages":    msgs,
		"stream":      true,
		"temperature": 0.7,
	})
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		lastErr = c.streamOnce(ctx, body, onChunk)
		if lastErr == nil {
			return nil
		}
		// Retry only transient HTTP errors (5xx, network). Never retry 4xx.
		var he *httpStatusError
		if errors.As(lastErr, &he) && he.code >= 400 && he.code < 500 {
			return lastErr
		}
	}
	return fmt.Errorf("%w: %v (after %d retries)", ErrStream, lastErr, c.cfg.MaxRetries)
}

type httpStatusError struct {
	code int
	body string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("http %d: %s", e.code, e.body)
}

func (c *Client) streamOnce(ctx context.Context, body []byte, onChunk func(Chunk)) error {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return &httpStatusError{code: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}

	firstToken := false
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}

		var ev struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue // ignore keep-alive or partial lines
		}
		if len(ev.Choices) == 0 {
			continue
		}
		ch := ev.Choices[0]
		delta := Chunk{Content: ch.Delta.Content, Reasoning: ch.Delta.ReasoningContent}
		if !firstToken && (delta.Content != "" || delta.Reasoning != "") {
			firstToken = true
			delta.Latency = time.Since(start)
		}
		if ch.FinishReason != nil && *ch.FinishReason == "stop" {
			delta.Finish = true
		}
		if delta.Content != "" || delta.Reasoning != "" || delta.Finish {
			onChunk(delta)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%w: read: %v", ErrStream, err)
	}
	return nil
}
