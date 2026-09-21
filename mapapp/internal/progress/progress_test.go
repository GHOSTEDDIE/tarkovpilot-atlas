package progress

import (
	"testing"
	"time"
)

func TestEventMergeRulesAndIsolation(t *testing.T) {
	book := NewBook()
	t1 := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	scope := Scope{Profile: "one", Mode: "pvp", Wipe: "wipe-a"}

	if !book.ApplyEvent(TaskEvent{EventID: "a", Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: "task", Status: StatusFailed, Source: "live-log", OccurredAt: t1}, t1).Applied {
		t.Fatal("initial event not applied")
	}
	if !book.ApplyEvent(TaskEvent{EventID: "b", Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: "task", Status: StatusStarted, Source: "live-log", OccurredAt: t2}, t2).Applied {
		t.Fatal("new start should recover a failed task")
	}
	if !book.ApplyEvent(TaskEvent{EventID: "c", Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: "task", Status: StatusCompleted, Source: "live-log", OccurredAt: t2.Add(time.Minute)}, t2).Applied {
		t.Fatal("completion not applied")
	}
	if got := book.ApplyEvent(TaskEvent{EventID: "d", Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: "task", Status: StatusFailed, Source: "history-log", OccurredAt: t2.Add(2 * time.Minute)}, t2); got.Applied {
		t.Fatal("completed task was auto-downgraded")
	}
	if got := book.ApplyEvent(TaskEvent{EventID: "c", Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: "task", Status: StatusCompleted, Source: "live-log", OccurredAt: t2}, t2); !got.Duplicate {
		t.Fatal("duplicate event was not recognized")
	}
	if !book.SetTask(scope, "task", StatusStarted, "manual", t2.Add(3*time.Minute)).Applied {
		t.Fatal("manual correction should be allowed")
	}
	other := book.Snapshot(Scope{Profile: "one", Mode: "pve", Wipe: "wipe-a"})
	if len(other.Tasks) != 0 {
		t.Fatal("PvP progress leaked into PvE")
	}
}

func TestOlderHistoryCannotReplaceNewerManualState(t *testing.T) {
	book := NewBook()
	scope := Scope{}
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	book.SetTask(scope, "task", StatusStarted, "manual", newer)
	result := book.ApplyEvent(TaskEvent{EventID: "old", TaskID: "task", Status: StatusFailed,
		Source: "history-log", OccurredAt: newer.Add(-time.Hour)}, newer)
	if result.Applied || book.Snapshot(scope).Tasks["task"].Status != StatusStarted {
		t.Fatal("older history replaced newer manual state")
	}
}
