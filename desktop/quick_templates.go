package main

// QuickTemplate is one entry of the user's quick-action template list.
// Mirrors the TS interface in desktop/frontend/src/lib/templates.ts —
// changes here MUST be reflected there (and in web/src/shared/templates.ts).
type QuickTemplate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// GetQuickTemplates returns the user's persisted templates. Always
// returns a non-nil slice; an empty slice means "no customisation,
// renderer falls back to DEFAULT_TEMPLATES".
func (a *App) GetQuickTemplates() []QuickTemplate {
	if a.cfgStore == nil {
		return []QuickTemplate{}
	}
	cfg := a.cfgStore.Get()
	if cfg.QuickTemplates == nil {
		return []QuickTemplate{}
	}
	// Return a shallow copy so callers can't mutate config-store state.
	out := make([]QuickTemplate, len(cfg.QuickTemplates))
	copy(out, cfg.QuickTemplates)
	return out
}

// SetQuickTemplates persists list verbatim. Passing nil or an empty
// slice clears storage; next GetQuickTemplates returns []. Caller is
// responsible for any validation (labels non-empty, no duplicate IDs,
// etc.) — this method is a thin pass-through.
func (a *App) SetQuickTemplates(list []QuickTemplate) error {
	if a.cfgStore == nil {
		return nil
	}
	cfg := a.cfgStore.Get()
	if len(list) == 0 {
		cfg.QuickTemplates = nil
	} else {
		cfg.QuickTemplates = list
	}
	return a.cfgStore.Set(cfg)
}
