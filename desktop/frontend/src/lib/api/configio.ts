// Config export/import (roadmap item config-export-import, task 4).
// Thin wrappers over the three Wails bindings in _bindings.ts -- see
// desktop/config_export.go and desktop/config_import.go for the Go side
// this mirrors. Desktop-only: callers must gate on
// platform.caps.wailsBindings before calling any of these (see
// SettingsConfigIO.vue), the same convention every other domain file here
// follows -- bindings() throws its own i18n-keyed error if called without a
// Wails window.go bridge present, but a UI that let a user get that far
// already failed to gate correctly.

import { bindings } from "./_bindings";
import type { ImportChange, ImportPreview, ImportReport } from "./_bindings";

export type { ImportChange, ImportPreview, ImportReport } from "./_bindings";

// exportConfig opens a native save dialog and writes the current config
// (SSH hosts, session profiles, and every customized synced preference) to
// the chosen path. includeLocalEnv=false (the default the UI should offer)
// strips profile Env for profiles that never opted into syncing it.
// Resolves to "" (not a rejection) when the user dismisses the dialog.
export function exportConfig(includeLocalEnv: boolean): Promise<string> {
  return bindings().ExportConfig(includeLocalEnv);
}

// previewConfigImport reports what applying jsonText WOULD change, without
// writing anything. Always call this before applyConfigImport -- it is also
// how the confirmation UI decides what to show.
export function previewConfigImport(jsonText: string): Promise<ImportPreview> {
  return bindings().PreviewConfigImport(jsonText);
}

// applyConfigImport writes every add/replace entry an equivalent
// previewConfigImport(jsonText) call would report. Must only be called after
// the user has seen that preview and explicitly confirmed -- see
// SettingsConfigIO.vue.
export function applyConfigImport(jsonText: string): Promise<ImportReport> {
  return bindings().ApplyConfigImport(jsonText);
}
