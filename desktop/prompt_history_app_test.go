package main

import (
	"reflect"
	"testing"
)

func TestAppPromptHistoryBridge_RoundTripsSettingsAndQueries(t *testing.T) {
	store, _ := tempPromptHistoryStore(t)
	a := &App{promptHistory: store}
	if err := store.Record("push and create mr"); err != nil {
		t.Fatal(err)
	}

	if got := a.GetPromptHistoryStatus(); !got.Enabled || got.Count != 1 {
		t.Fatalf("GetPromptHistoryStatus = %+v", got)
	}
	if got := a.QueryPromptHistory("push", 3); !reflect.DeepEqual(got, []string{"push and create mr"}) {
		t.Fatalf("QueryPromptHistory = %q", got)
	}
	page, err := a.QueryPromptHistoryPage("", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || !reflect.DeepEqual(page.Items, []PromptHistoryItem{{Text: "push and create mr", LastUsedAt: store.entries[0].LastUsedAt}}) {
		t.Fatalf("QueryPromptHistoryPage = %+v", page)
	}
	if err := a.SetPromptHistoryEnabled(false); err != nil {
		t.Fatal(err)
	}
	if got := a.QueryPromptHistory("push", 3); len(got) != 0 {
		t.Fatalf("disabled QueryPromptHistory = %q", got)
	}
	if err := a.ClearPromptHistory(); err != nil {
		t.Fatal(err)
	}
	if got := a.GetPromptHistoryStatus(); got.Enabled || got.Count != 0 {
		t.Fatalf("status after disabled clear = %+v", got)
	}
}

func TestAppPromptHistoryBridge_IsUnavailableWithoutStore(t *testing.T) {
	a := &App{}
	if got := a.GetPromptHistoryStatus(); got.Enabled || got.Count != 0 {
		t.Fatalf("nil store status = %+v", got)
	}
	if got := a.QueryPromptHistory("push", 3); len(got) != 0 {
		t.Fatalf("nil store query = %q", got)
	}
	page, err := a.QueryPromptHistoryPage("", "", 5)
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("nil store page = %+v, err=%v", page, err)
	}
	if err := a.SetPromptHistoryEnabled(true); err == nil {
		t.Fatal("nil store enable should report unavailable")
	}
	if err := a.ClearPromptHistory(); err == nil {
		t.Fatal("nil store clear should report unavailable")
	}
}
