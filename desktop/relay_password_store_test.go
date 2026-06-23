package main

import "testing"

func TestSaveLoadRelayPassword_RoundTrip(t *testing.T) {
	if err := saveRelayPassword("https://r.example.com", "u@example.com", "hunter2"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadRelayPassword("https://r.example.com", "u@example.com")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("got %q want %q", got, "hunter2")
	}
}

func TestLoadRelayPassword_NotFound(t *testing.T) {
	got, err := loadRelayPassword("https://nobody.example.com", "ghost@example.com")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestSaveRelayPassword_EmptyOriginOrEmail_NoOp(t *testing.T) {
	if err := saveRelayPassword("https://r.example.com", "keep@example.com", "keeper"); err != nil {
		t.Fatalf("save baseline: %v", err)
	}
	if err := saveRelayPassword("", "keep@example.com", "intruder"); err != nil {
		t.Fatalf("save empty origin: %v", err)
	}
	if err := saveRelayPassword("https://r.example.com", "", "intruder"); err != nil {
		t.Fatalf("save empty email: %v", err)
	}
	got, _ := loadRelayPassword("https://r.example.com", "keep@example.com")
	if got != "keeper" {
		t.Fatalf("baseline slot mutated: got %q want %q", got, "keeper")
	}
}

func TestClearRelayPassword_RoundTrip(t *testing.T) {
	if err := saveRelayPassword("https://r.example.com", "u2@example.com", "pw"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := clearRelayPasswordFor("https://r.example.com", "u2@example.com"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ := loadRelayPassword("https://r.example.com", "u2@example.com")
	if got != "" {
		t.Fatalf("got %q want empty after clear", got)
	}
}

func TestClearRelayPassword_NotFound_NoError(t *testing.T) {
	if err := clearRelayPasswordFor("https://r.example.com", "never@example.com"); err != nil {
		t.Fatalf("clear: %v", err)
	}
}

func TestSaveRelayPassword_NormalizesOriginAndEmail(t *testing.T) {
	if err := saveRelayPassword("https://r.example.com/", " u3@example.com ", "pw3"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, _ := loadRelayPassword("https://r.example.com", "u3@example.com")
	if got != "pw3" {
		t.Fatalf("got %q want %q (normalize)", got, "pw3")
	}
}

func TestSaveRelayPassword_EmptyPasswordDeletes(t *testing.T) {
	if err := saveRelayPassword("https://r.example.com", "del@example.com", "before"); err != nil {
		t.Fatalf("save before: %v", err)
	}
	if err := saveRelayPassword("https://r.example.com", "del@example.com", ""); err != nil {
		t.Fatalf("save empty (should delete): %v", err)
	}
	got, _ := loadRelayPassword("https://r.example.com", "del@example.com")
	if got != "" {
		t.Fatalf("slot not cleared: got %q", got)
	}
}

func TestSaveRelayPassword_OverwritesExistingSlot(t *testing.T) {
	const origin = "https://r.example.com"
	const email = "u@example.com"
	if err := saveRelayPassword(origin, email, "first"); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := saveRelayPassword(origin, email, "second"); err != nil {
		t.Fatalf("save second: %v", err)
	}
	got, err := loadRelayPassword(origin, email)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "second" {
		t.Fatalf("got %q want %q after overwrite", got, "second")
	}
}
