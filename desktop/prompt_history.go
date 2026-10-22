package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/attson/atterm/internal/appdir"
)

const (
	promptHistoryVersion        = 1
	promptHistoryMaxEntries     = 200
	promptHistoryMaxPromptBytes = 4 * 1024
	promptHistoryTTL            = 180 * 24 * time.Hour
	promptHistoryMaxSuggestions = 3
)

type promptHistoryEntry struct {
	Text       string `json:"text"`
	Count      int    `json:"count"`
	LastUsedAt int64  `json:"last_used_at"`
}

type promptHistoryDocument struct {
	Version int                  `json:"version"`
	Enabled *bool                `json:"enabled,omitempty"`
	Entries []promptHistoryEntry `json:"entries,omitempty"`
}

// PromptHistoryStatus is the desktop-local state shown in Settings.
type PromptHistoryStatus struct {
	Enabled bool `json:"enabled"`
	Count   int  `json:"count"`
}

// PromptHistoryStore owns the local AI prompt history document. Querying is
// entirely in memory so a slow or unavailable disk can never delay PTY input.
type PromptHistoryStore struct {
	mu      sync.RWMutex
	path    string
	now     func() time.Time
	enabled bool
	entries []promptHistoryEntry
}

func NewPromptHistoryStore() (*PromptHistoryStore, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return nil, err
	}
	return newPromptHistoryStore(filepath.Join(dir, "prompt-history.json"), time.Now), nil
}

func newPromptHistoryStore(path string, now func() time.Time) *PromptHistoryStore {
	s := &PromptHistoryStore{path: path, now: now, enabled: true}
	s.load()
	return s
}

func (s *PromptHistoryStore) load() {
	blob, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var doc promptHistoryDocument
	if json.Unmarshal(blob, &doc) != nil || doc.Version != promptHistoryVersion {
		return
	}
	if doc.Enabled != nil {
		s.enabled = *doc.Enabled
	}
	s.entries = doc.Entries
	s.pruneLocked(s.now())
}

func (s *PromptHistoryStore) Record(text string) error {
	if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || len([]byte(text)) > promptHistoryMaxPromptBytes {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return nil
	}
	now := s.now()
	for i := range s.entries {
		if s.entries[i].Text == text {
			s.entries[i].Count++
			s.entries[i].LastUsedAt = now.Unix()
			s.pruneLocked(now)
			return s.saveLocked()
		}
	}
	s.entries = append(s.entries, promptHistoryEntry{Text: text, Count: 1, LastUsedAt: now.Unix()})
	s.pruneLocked(now)
	return s.saveLocked()
}

// RecordPrompt implements feishu.PromptSink without coupling that ingress
// package to the desktop store type.
func (s *PromptHistoryStore) RecordPrompt(prompt string) error {
	return s.Record(prompt)
}

// GetPromptHistoryStatus returns the local suggestion setting and entry count.
func (a *App) GetPromptHistoryStatus() PromptHistoryStatus {
	if a.promptHistory == nil {
		return PromptHistoryStatus{}
	}
	return a.promptHistory.Status()
}

// QueryPromptHistory returns at most three deterministic prefix matches.
func (a *App) QueryPromptHistory(prefix string, limit int) []string {
	if a.promptHistory == nil {
		return []string{}
	}
	return a.promptHistory.Query(prefix, limit)
}

// SetPromptHistoryEnabled updates the device-local collection preference.
func (a *App) SetPromptHistoryEnabled(enabled bool) error {
	if a.promptHistory == nil {
		return errors.New("prompt history unavailable")
	}
	return a.promptHistory.SetEnabled(enabled)
}

// ClearPromptHistory removes all locally stored prompts while preserving the
// enabled preference.
func (a *App) ClearPromptHistory() error {
	if a.promptHistory == nil {
		return errors.New("prompt history unavailable")
	}
	return a.promptHistory.Clear()
}

func (s *PromptHistoryStore) Query(prefix string, limit int) []string {
	prefix = strings.TrimLeft(prefix, " \t\r\n")
	if utf8.RuneCountInString(prefix) < 2 || limit <= 0 {
		return []string{}
	}
	if limit > promptHistoryMaxSuggestions {
		limit = promptHistoryMaxSuggestions
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.enabled {
		return []string{}
	}
	cutoff := s.now().Add(-promptHistoryTTL).Unix()
	type match struct {
		text       string
		count      int
		lastUsedAt int64
	}
	matches := make([]match, 0, limit)
	for _, entry := range s.entries {
		if entry.LastUsedAt < cutoff || !asciiFoldHasPrefix(entry.Text, prefix) || len(entry.Text) == len(prefix) {
			continue
		}
		matches = append(matches, match{entry.Text, entry.Count, entry.LastUsedAt})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].count != matches[j].count {
			return matches[i].count > matches[j].count
		}
		if matches[i].lastUsedAt != matches[j].lastUsedAt {
			return matches[i].lastUsedAt > matches[j].lastUsedAt
		}
		return matches[i].text < matches[j].text
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]string, len(matches))
	for i := range matches {
		out[i] = matches[i].text
	}
	return out
}

func (s *PromptHistoryStore) Status() PromptHistoryStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := s.now().Add(-promptHistoryTTL).Unix()
	count := 0
	for _, entry := range s.entries {
		if entry.LastUsedAt >= cutoff {
			count++
		}
	}
	return PromptHistoryStatus{Enabled: s.enabled, Count: count}
}

func (s *PromptHistoryStore) SetEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.enabled
	s.enabled = enabled
	if err := s.saveLocked(); err != nil {
		s.enabled = previous
		return err
	}
	return nil
}

func (s *PromptHistoryStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.entries
	s.entries = nil
	if err := s.saveLocked(); err != nil {
		s.entries = previous
		return err
	}
	return nil
}

func (s *PromptHistoryStore) pruneLocked(now time.Time) {
	cutoff := now.Add(-promptHistoryTTL).Unix()
	kept := s.entries[:0]
	for _, entry := range s.entries {
		if entry.Text == "" || !utf8.ValidString(entry.Text) || len([]byte(entry.Text)) > promptHistoryMaxPromptBytes || entry.LastUsedAt < cutoff {
			continue
		}
		if entry.Count < 1 {
			entry.Count = 1
		}
		kept = append(kept, entry)
	}
	s.entries = kept
	if len(s.entries) <= promptHistoryMaxEntries {
		return
	}
	sort.Slice(s.entries, func(i, j int) bool {
		if s.entries[i].LastUsedAt != s.entries[j].LastUsedAt {
			return s.entries[i].LastUsedAt > s.entries[j].LastUsedAt
		}
		return s.entries[i].Text < s.entries[j].Text
	})
	s.entries = s.entries[:promptHistoryMaxEntries]
}

func (s *PromptHistoryStore) saveLocked() error {
	enabled := s.enabled
	doc := promptHistoryDocument{
		Version: promptHistoryVersion,
		Enabled: &enabled,
		Entries: s.entries,
	}
	blob, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal prompt history: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create prompt history directory: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("write prompt history: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("set prompt history permissions: %w", err)
	}
	if f, err := os.Open(tmp); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace prompt history: %w", err)
	}
	return nil
}

func asciiFoldHasPrefix(text, prefix string) bool {
	if len(prefix) > len(text) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		a, b := text[i], prefix[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
