package session

import (
	"encoding/json"

	"github.com/attson/atterm/internal/proto"
)

func encodeMetaPayload(meta proto.SessionInfo, driverClientID, driverClientName string) ([]byte, error) {
	return json.Marshal(proto.MetaPayload{
		Cwd:               meta.Cwd,
		Title:             meta.Title,
		DriverClientID:    driverClientID,
		DriverClientName:  driverClientName,
		Cols:              meta.Cols,
		Rows:              meta.Rows,
		TaskState:         meta.TaskState,
		CurrentCommand:    meta.CurrentCommand,
		CommandStartedAt:  meta.CommandStartedAt,
		CommandEndedAt:    meta.CommandEndedAt,
		CommandDurationMS: meta.CommandDurationMS,
		CommandExitCode:   meta.CommandExitCode,
		LastOutputAt:      meta.LastOutputAt,
		Type:              meta.Type,
		Summary:           meta.Summary,
		AttentionAt:       meta.AttentionAt,
	})
}

// isAttentionType reports whether a session whose workload Type is t should
// generate an inbox entry when it finishes. Empty Type means shell.
func isAttentionType(t string) bool {
	return t != "" && t != SessionTypeShell
}

// DriverClientID returns the end-to-end client_id of the current driver, or
// "" if no driver is assigned.

func mergeTaskMeta(meta *proto.SessionInfo, m proto.MetaPayload) bool {
	info := proto.SessionInfo{
		TaskState:         m.TaskState,
		CurrentCommand:    m.CurrentCommand,
		CommandStartedAt:  m.CommandStartedAt,
		CommandEndedAt:    m.CommandEndedAt,
		CommandDurationMS: m.CommandDurationMS,
		CommandExitCode:   m.CommandExitCode,
		LastOutputAt:      m.LastOutputAt,
	}
	return mergeTaskInfo(meta, info)
}

func mergeTaskInfo(meta *proto.SessionInfo, info proto.SessionInfo) bool {
	if !hasTaskInfo(info) {
		return false
	}
	changed := false
	if info.TaskState != "" && meta.TaskState != info.TaskState {
		meta.TaskState = info.TaskState
		changed = true
	}
	if info.CurrentCommand != meta.CurrentCommand {
		meta.CurrentCommand = info.CurrentCommand
		changed = true
	}
	if info.CommandStartedAt != 0 && meta.CommandStartedAt != info.CommandStartedAt {
		meta.CommandStartedAt = info.CommandStartedAt
		changed = true
	}
	if info.CommandEndedAt != meta.CommandEndedAt {
		meta.CommandEndedAt = info.CommandEndedAt
		changed = true
	}
	if info.CommandDurationMS != meta.CommandDurationMS {
		meta.CommandDurationMS = info.CommandDurationMS
		changed = true
	}
	if !sameOptionalInt(meta.CommandExitCode, info.CommandExitCode) {
		meta.CommandExitCode = cloneOptionalInt(info.CommandExitCode)
		changed = true
	}
	if info.LastOutputAt != 0 && meta.LastOutputAt != info.LastOutputAt {
		meta.LastOutputAt = info.LastOutputAt
		changed = true
	}
	if !sameSummary(meta.Summary, info.Summary) {
		meta.Summary = cloneSummary(info.Summary)
		changed = true
	}
	return changed
}

func hasTaskInfo(info proto.SessionInfo) bool {
	return info.TaskState != "" ||
		info.CurrentCommand != "" ||
		info.CommandStartedAt != 0 ||
		info.CommandEndedAt != 0 ||
		info.CommandDurationMS != 0 ||
		info.CommandExitCode != nil ||
		info.LastOutputAt != 0 ||
		info.Summary != nil
}

// sameSummary reports whether two SessionSummary pointers carry equivalent
// content. Both nil → equal; one nil → unequal; both non-nil → field-wise.
// Used by ANNOUNCE reconcile to avoid unnecessary META broadcasts when the
// agent re-announces an unchanged summary.
func sameSummary(a, b *proto.SessionSummary) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.RecentOutput != b.RecentOutput || a.CapturedAt != b.CapturedAt {
		return false
	}
	if len(a.ErrorLines) != len(b.ErrorLines) {
		return false
	}
	for i := range a.ErrorLines {
		if a.ErrorLines[i] != b.ErrorLines[i] {
			return false
		}
	}
	return true
}

// cloneSummary deep-copies the slice so a future mutation on the inbound
// ANNOUNCE struct does not leak into the mirror session's meta.
func cloneSummary(s *proto.SessionSummary) *proto.SessionSummary {
	if s == nil {
		return nil
	}
	out := &proto.SessionSummary{
		RecentOutput: s.RecentOutput,
		CapturedAt:   s.CapturedAt,
	}
	if len(s.ErrorLines) > 0 {
		out.ErrorLines = append([]string(nil), s.ErrorLines...)
	}
	return out
}

func sameOptionalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cloneOptionalInt(v *int) *int {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
