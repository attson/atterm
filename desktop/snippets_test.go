package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSnippetsRoundTripAndCopyIsolation(t *testing.T) {
	a := newRelayTestApp(t)
	want := []Snippet{
		{ID: "one", Label: "Ports", Text: "lsof -nP -iTCP -sTCP:LISTEN"},
		{ID: "two", Label: "Hosts", Text: "cat >> /etc/hosts <<'EOF'\n127.0.0.1 local\nEOF"},
	}
	if err := a.SetSnippets(want); err != nil {
		t.Fatalf("SetSnippets: %v", err)
	}
	got := a.GetSnippets()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got=%#v\nwant=%#v", got, want)
	}
	got[0].Label = "mutated"
	if again := a.GetSnippets(); again[0].Label != "Ports" {
		t.Fatalf("GetSnippets exposed config state: %#v", again)
	}
}

func TestSetSnippetsRejectsInvalidCollectionsWithoutReplacingSavedValue(t *testing.T) {
	a := newRelayTestApp(t)
	saved := []Snippet{{ID: "saved", Label: "Saved", Text: "echo saved"}}
	if err := a.SetSnippets(saved); err != nil {
		t.Fatal(err)
	}

	tests := map[string][]Snippet{
		"empty id":       {{ID: "", Label: "Label", Text: "echo hi"}},
		"empty label":    {{ID: "id", Label: " \t", Text: "echo hi"}},
		"empty text":     {{ID: "id", Label: "Label", Text: ""}},
		"duplicate id":   {{ID: "dup", Label: "A", Text: "a"}, {ID: "dup", Label: "B", Text: "b"}},
		"label too long": {{ID: "id", Label: strings.Repeat("界", snippetMaxLabelCodePoints+1), Text: "echo hi"}},
		"text too long":  {{ID: "id", Label: "Label", Text: strings.Repeat("x", snippetMaxTextBytes+1)}},
	}
	many := make([]Snippet, snippetMaxItems+1)
	for i := range many {
		many[i] = Snippet{ID: string(rune(0x1000 + i)), Label: "Label", Text: "echo hi"}
	}
	tests["too many"] = many

	for name, list := range tests {
		t.Run(name, func(t *testing.T) {
			if err := a.SetSnippets(list); err == nil {
				t.Fatal("expected validation error")
			}
			if got := a.GetSnippets(); !reflect.DeepEqual(got, saved) {
				t.Fatalf("invalid write replaced saved snippets: %#v", got)
			}
		})
	}
}

func TestSetSnippetsRejectsSerializedCollectionOverOneMiB(t *testing.T) {
	a := newRelayTestApp(t)
	items := make([]Snippet, 17)
	for i := range items {
		items[i] = Snippet{
			ID:    string(rune('a' + i)),
			Label: "Large",
			Text:  strings.Repeat("x", snippetMaxTextBytes),
		}
	}
	if err := a.SetSnippets(items); err == nil {
		t.Fatal("expected collection-size error")
	}
}

func TestSetSnippetsEmptyClears(t *testing.T) {
	a := newRelayTestApp(t)
	if err := a.SetSnippets([]Snippet{{ID: "id", Label: "Label", Text: "echo hi"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSnippets(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := a.GetSnippets(); len(got) != 0 {
		t.Fatalf("after clear: %#v", got)
	}
}
