// Package progress owns profile-scoped task state and the conflict rules for
// manual, live-log, and historical-log updates.
package progress

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const SchemaVersion = 1

const (
	StatusUntracked = "untracked"
	StatusStarted   = "started"
	StatusFailed    = "failed"
	StatusCompleted = "completed"
)

type Scope struct {
	Profile string `json:"profile"`
	Mode    string `json:"mode"`
	Wipe    string `json:"wipe"`
}

func NormalizeScope(scope Scope) Scope {
	if strings.TrimSpace(scope.Profile) == "" {
		scope.Profile = "default"
	}
	if scope.Mode != "pve" {
		scope.Mode = "pvp"
	}
	if strings.TrimSpace(scope.Wipe) == "" {
		scope.Wipe = "current"
	}
	return scope
}

func (s Scope) Key() string {
	s = NormalizeScope(s)
	return s.Mode + "\x00" + s.Profile + "\x00" + s.Wipe
}

type Record struct {
	Status     string    `json:"status"`
	Source     string    `json:"source"`
	OccurredAt time.Time `json:"occurredAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ProfileState struct {
	Scope      Scope             `json:"scope"`
	Tasks      map[string]Record `json:"tasks"`
	Objectives map[string]Record `json:"objectives"`
}

type Book struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Profiles      map[string]*ProfileState `json:"profiles"`
	SeenEvents    map[string]time.Time     `json:"seenEvents,omitempty"`
}

type TaskEvent struct {
	EventID    string    `json:"eventId,omitempty"`
	Profile    string    `json:"profile,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Wipe       string    `json:"wipe,omitempty"`
	TaskID     string    `json:"taskId"`
	Status     string    `json:"status"`
	Source     string    `json:"source,omitempty"`
	OccurredAt time.Time `json:"occurredAt,omitempty"`
	FileID     string    `json:"fileId,omitempty"`
	Offset     int64     `json:"offset,omitempty"`
}

type ApplyResult struct {
	Applied   bool   `json:"applied"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func NewBook() *Book {
	return &Book{SchemaVersion: SchemaVersion, Profiles: map[string]*ProfileState{}, SeenEvents: map[string]time.Time{}}
}

func (b *Book) Ensure() {
	if b.SchemaVersion == 0 {
		b.SchemaVersion = SchemaVersion
	}
	if b.Profiles == nil {
		b.Profiles = map[string]*ProfileState{}
	}
	if b.SeenEvents == nil {
		b.SeenEvents = map[string]time.Time{}
	}
}

func (b *Book) State(scope Scope) *ProfileState {
	b.Ensure()
	scope = NormalizeScope(scope)
	state := b.Profiles[scope.Key()]
	if state == nil {
		state = &ProfileState{Scope: scope, Tasks: map[string]Record{}, Objectives: map[string]Record{}}
		b.Profiles[scope.Key()] = state
	}
	if state.Tasks == nil {
		state.Tasks = map[string]Record{}
	}
	if state.Objectives == nil {
		state.Objectives = map[string]Record{}
	}
	return state
}

func (b *Book) Snapshot(scope Scope) ProfileState {
	state := b.State(scope)
	out := ProfileState{Scope: state.Scope, Tasks: map[string]Record{}, Objectives: map[string]Record{}}
	for id, record := range state.Tasks {
		out.Tasks[id] = record
	}
	for id, record := range state.Objectives {
		out.Objectives[id] = record
	}
	return out
}

func (b *Book) Clone() *Book {
	b.Ensure()
	out := NewBook()
	for key, state := range b.Profiles {
		copyState := &ProfileState{Scope: state.Scope, Tasks: map[string]Record{}, Objectives: map[string]Record{}}
		for id, record := range state.Tasks {
			copyState.Tasks[id] = record
		}
		for id, record := range state.Objectives {
			copyState.Objectives[id] = record
		}
		out.Profiles[key] = copyState
	}
	for id, seenAt := range b.SeenEvents {
		out.SeenEvents[id] = seenAt
	}
	return out
}

func ValidTaskStatus(status string) bool {
	switch status {
	case StatusUntracked, StatusStarted, StatusFailed, StatusCompleted:
		return true
	default:
		return false
	}
}

func (b *Book) ApplyEvent(event TaskEvent, now time.Time) ApplyResult {
	b.Ensure()
	if event.TaskID == "" || !ValidTaskStatus(event.Status) {
		return ApplyResult{Reason: "invalid task or status"}
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	if event.Source == "" {
		event.Source = "live-log"
	}
	if event.EventID == "" {
		fileID := event.FileID
		if fileID == "" {
			fileID = event.Source + ":" + event.OccurredAt.UTC().Format(time.RFC3339Nano)
		}
		event.EventID = EventKey(fileID, event.Offset, event.TaskID, event.Status)
	}
	if _, exists := b.SeenEvents[event.EventID]; exists {
		return ApplyResult{Duplicate: true, Reason: "duplicate event"}
	}
	b.SeenEvents[event.EventID] = now.UTC()

	scope := Scope{Profile: event.Profile, Mode: event.Mode, Wipe: event.Wipe}
	state := b.State(scope)
	current, exists := state.Tasks[event.TaskID]
	if exists && event.OccurredAt.Before(current.OccurredAt) {
		return ApplyResult{Reason: "older event"}
	}
	if exists && current.Status == StatusCompleted && event.Status != StatusCompleted && event.Source != "manual" {
		return ApplyResult{Reason: "completed tasks are not auto-downgraded"}
	}
	state.Tasks[event.TaskID] = Record{
		Status: event.Status, Source: event.Source,
		OccurredAt: event.OccurredAt.UTC(), UpdatedAt: now.UTC(),
	}
	return ApplyResult{Applied: true}
}

func (b *Book) SetTask(scope Scope, taskID, status, source string, now time.Time) ApplyResult {
	if source == "" {
		source = "manual"
	}
	return b.ApplyEvent(TaskEvent{
		Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: taskID, Status: status, Source: source, OccurredAt: now,
		EventID: "manual:" + scope.Key() + ":" + taskID + ":" + now.UTC().Format(time.RFC3339Nano),
	}, now)
}

func (b *Book) SetObjective(scope Scope, objectiveID string, completed bool, now time.Time) ApplyResult {
	if objectiveID == "" {
		return ApplyResult{Reason: "objective id required"}
	}
	state := b.State(scope)
	status := StatusUntracked
	if completed {
		status = StatusCompleted
	}
	state.Objectives[objectiveID] = Record{Status: status, Source: "manual", OccurredAt: now.UTC(), UpdatedAt: now.UTC()}
	return ApplyResult{Applied: true}
}

func EventKey(fileID string, offset int64, taskID, status string) string {
	raw := fmt.Sprintf("%s\x00%d\x00%s\x00%s", fileID, offset, taskID, status)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
