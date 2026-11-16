package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	snippetMaxLabelCodePoints = 80
	snippetMaxTextBytes       = 64 << 10
	snippetMaxItems           = 200
	snippetMaxCollectionBytes = 1 << 20
)

// Snippet is one long command template. It intentionally has no hotkey:
// execution must stay behind the explicit Run or Paste choice.
type Snippet struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// GetSnippets returns a detached copy of the persisted ordered snippet list.
func (a *App) GetSnippets() []Snippet {
	if a.cfgStore == nil {
		return []Snippet{}
	}
	stored := a.cfgStore.Get().Snippets
	if stored == nil {
		return []Snippet{}
	}
	out := make([]Snippet, len(stored))
	copy(out, stored)
	return out
}

// SetSnippets validates and persists the full ordered snippet list.
func (a *App) SetSnippets(list []Snippet) error {
	if err := validateSnippets(list); err != nil {
		return err
	}
	if a.cfgStore == nil {
		return nil
	}

	cfg := a.cfgStore.Get()
	if len(list) == 0 {
		cfg.Snippets = nil
	} else {
		cfg.Snippets = append([]Snippet(nil), list...)
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("snippets")
	return nil
}

func validateSnippets(list []Snippet) error {
	if len(list) > snippetMaxItems {
		return fmt.Errorf("snippets: at most %d entries are allowed", snippetMaxItems)
	}
	seen := make(map[string]struct{}, len(list))
	for _, snippet := range list {
		if strings.TrimSpace(snippet.ID) == "" {
			return errors.New("snippets: id is required")
		}
		if strings.TrimSpace(snippet.Label) == "" {
			return fmt.Errorf("snippet %q: label is required", snippet.ID)
		}
		if strings.TrimSpace(snippet.Text) == "" {
			return fmt.Errorf("snippet %q: command text is required", snippet.ID)
		}
		if !utf8.ValidString(snippet.Label) || !utf8.ValidString(snippet.Text) {
			return fmt.Errorf("snippet %q: label and command text must be valid UTF-8", snippet.ID)
		}
		if utf8.RuneCountInString(snippet.Label) > snippetMaxLabelCodePoints {
			return fmt.Errorf("snippet %q: label exceeds %d Unicode code points", snippet.ID, snippetMaxLabelCodePoints)
		}
		if len(snippet.Text) > snippetMaxTextBytes {
			return fmt.Errorf("snippet %q: command text exceeds %d bytes", snippet.ID, snippetMaxTextBytes)
		}
		if _, duplicate := seen[snippet.ID]; duplicate {
			return fmt.Errorf("snippets: duplicate id %q", snippet.ID)
		}
		seen[snippet.ID] = struct{}{}
	}

	encoded, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("snippets: encode: %w", err)
	}
	if len(encoded) > snippetMaxCollectionBytes {
		return fmt.Errorf("snippets: collection exceeds %d bytes", snippetMaxCollectionBytes)
	}
	return nil
}
