package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TranslateHTTPRequest is the payload the translate plugin sends from the
// renderer when it wants Go to proxy an OpenAI-compatible chat completion.
// WKWebView blocks direct fetch() to user-supplied third-party endpoints (no
// CORS, cert quirks), so the plugin routes through here on desktop.
//
// Body is passed opaquely — the renderer builds it fully (model, messages,
// temperature, any advanced params merged from user's extraParams) so this
// proxy stays agnostic to the wire schema.
type TranslateHTTPRequest struct {
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	Body           string `json:"body"` // serialized JSON body, sent as-is
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// TranslateHTTPResponse mirrors the raw HTTP reply so the renderer's OpenAI
// parser stays unchanged. When the upstream responds with SSE
// (stream: true), this method collapses the delta stream into a synthesized
// non-stream envelope so callers don't need SSE parsers.
type TranslateHTTPResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// TranslateOpenAIChat POSTs the given body to {BaseURL}/v1/chat/completions.
// Only transport-layer failures return an error; HTTP status codes are
// surfaced verbatim in Status so the frontend can map them to translate
// error codes.
func (a *App) TranslateOpenAIChat(req TranslateHTTPRequest) (TranslateHTTPResponse, error) {
	if strings.TrimSpace(req.BaseURL) == "" {
		return TranslateHTTPResponse{}, fmt.Errorf("baseUrl required")
	}
	if strings.TrimSpace(req.Body) == "" {
		return TranslateHTTPResponse{}, fmt.Errorf("body required")
	}

	url := strings.TrimRight(req.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader([]byte(req.Body)))
	if err != nil {
		return TranslateHTTPResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	resp, err := client.Do(httpReq)
	if err != nil {
		return TranslateHTTPResponse{}, err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.HasPrefix(ct, "text/event-stream") {
		collapsed, err := collapseOpenAISSE(resp.Body)
		if err != nil {
			return TranslateHTTPResponse{}, err
		}
		return TranslateHTTPResponse{Status: resp.StatusCode, Body: collapsed}, nil
	}

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return TranslateHTTPResponse{}, err
	}
	return TranslateHTTPResponse{Status: resp.StatusCode, Body: string(rawBody)}, nil
}

// collapseOpenAISSE reads an OpenAI-shaped SSE stream (data: {...}\n\n ...
// data: [DONE]) and returns a synthesized non-stream chat/completions
// envelope so the renderer's existing parser can consume it unchanged.
func collapseOpenAISSE(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	// Allow up to 1MiB per line — SSE chunks are small but tool_calls can
	// grow. Default 64KiB would truncate long payloads silently.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var content strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				break
			}
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message *struct {
					Content string `json:"content"`
				} `json:"message,omitempty"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Non-JSON data line — ignore, don't fail the whole stream.
			continue
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				content.WriteString(c.Delta.Content)
			} else if c.Message != nil && c.Message.Content != "" {
				content.WriteString(c.Message.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	envelope := map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"content": content.String()}},
		},
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
