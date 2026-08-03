package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCollapseOpenAISSE_JoinsDeltaContent(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":" world"}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	out, err := collapseOpenAISSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("collapse: %v", err)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v (body=%q)", err, out)
	}
	if len(envelope.Choices) != 1 {
		t.Fatalf("want 1 choice, got %d", len(envelope.Choices))
	}
	if got := envelope.Choices[0].Message.Content; got != "Hello world" {
		t.Fatalf("content = %q, want %q", got, "Hello world")
	}
}

func TestCollapseOpenAISSE_IgnoresNonJSONAndUnknownEvents(t *testing.T) {
	stream := strings.Join([]string{
		`: keep-alive`,
		`event: ping`,
		`data: not-json`,
		``,
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	out, err := collapseOpenAISSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("collapse: %v", err)
	}
	if !strings.Contains(out, `"content":"ok"`) {
		t.Fatalf("expected content 'ok' in envelope, got %q", out)
	}
}

func TestCollapseOpenAISSE_MessageFallback(t *testing.T) {
	// Some proxies emit full message objects rather than delta chunks.
	stream := strings.Join([]string{
		`data: {"choices":[{"message":{"content":"complete answer"}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	out, err := collapseOpenAISSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("collapse: %v", err)
	}
	if !strings.Contains(out, `"content":"complete answer"`) {
		t.Fatalf("expected 'complete answer' in envelope, got %q", out)
	}
}
