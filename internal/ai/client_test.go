package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sseServer returns an httptest server that speaks OpenAI-style SSE with the
// given chunks, optionally delaying the first one to exercise TTFT timing.
func sseServer(chunks []string, firstDelay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "wrong path: "+r.URL.Path, 404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad auth", 401)
			return
		}
		if firstDelay > 0 {
			time.Sleep(firstDelay)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			if _, err := io.WriteString(w, "data: "+c+"\n\n"); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
}

func TestStreamChat(t *testing.T) {
	srv := sseServer([]string{
		`{"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`,
	}, 0)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "test-key", Model: "test-model"})

	var got []string
	finished := false
	err := c.StreamChat(context.Background(), "sys", nil, func(ch Chunk) {
		if ch.Finish {
			finished = true
		}
		if ch.Content != "" {
			got = append(got, ch.Content)
		}
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if strings.Join(got, "") != "Hello world" {
		t.Fatalf("content = %q, want %q", strings.Join(got, ""), "Hello world")
	}
	if !finished {
		t.Fatal("expected finish flag on final chunk")
	}
}

func TestStreamChat_TTFT(t *testing.T) {
	const delay = 80 * time.Millisecond
	srv := sseServer([]string{
		`{"choices":[{"delta":{"content":"first"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":" second"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`,
	}, delay)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "test-key", Model: "m"})

	var ttft time.Duration
	err := c.StreamChat(context.Background(), "sys", nil, func(ch Chunk) {
		if ch.Latency > 0 {
			ttft = ch.Latency
		}
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if ttft < delay {
		t.Fatalf("ttft = %v, want >= %v", ttft, delay)
	}
}

func TestStreamChat_HTTPErrorNoRetry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"error":{"message":"bad key"}}`, 401)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "test-key", Model: "m", MaxRetries: 3})
	err := c.StreamChat(context.Background(), "sys", nil, func(Chunk) {})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (4xx must not retry)", calls)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want mention of 401", err)
	}
}

func TestStreamChat_RetriesOn500(t *testing.T) {
	var calls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if calls == 1 {
			http.Error(w, "boom", 500)
			return
		}
		io.WriteString(w, "data: "+`{"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
		io.WriteString(w, "data: "+`{"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "test-key", Model: "m", MaxRetries: 1})
	err := c.StreamChat(context.Background(), "sys", nil, func(Chunk) {})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestStreamChat_ContextCancelled(t *testing.T) {
	srv := sseServer([]string{
		`{"choices":[{"delta":{"content":"slow"},"finish_reason":null}]}`,
	}, 5*time.Second)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "test-key", Model: "m"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.StreamChat(ctx, "sys", nil, func(Chunk) {})
	if err == nil {
		t.Fatal("expected context error")
	}
}
