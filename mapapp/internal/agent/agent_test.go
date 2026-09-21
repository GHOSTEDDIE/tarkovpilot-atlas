package agent

import (
	"os"
	"path/filepath"
	"testing"

	"tarkovmap/internal/progress"
)

func TestQuestStatusFromTemplate(t *testing.T) {
	cases := []struct {
		templateID string
		rawType    string
		wantID     string
		wantStatus string
		wantOK     bool
	}{
		{"59c124d686f774189b3c843f successMessageText", "24", "59c124d686f774189b3c843f", "completed", true},
		{"59c124d686f774189b3c843f failMessageText", "24", "59c124d686f774189b3c843f", "failed", true},
		{"59c124d686f774189b3c843f startMessageText", "24", "59c124d686f774189b3c843f", "started", true},
		{"59c124d686f774189b3c843f acceptMessageText", "24", "59c124d686f774189b3c843f", "started", true},
		{"59c124d686f774189b3c843f somethingElse", "10", "59c124d686f774189b3c843f", "started", true},
		{"59c124d686f774189b3c843f somethingElse", "11", "59c124d686f774189b3c843f", "failed", true},
		{"59c124d686f774189b3c843f somethingElse", "12", "59c124d686f774189b3c843f", "completed", true},
		{"59c124d686f774189b3c843f somethingElse", "24", "", "", false},
		{"59c124d686f774189b3c843f", "24", "", "", false},
		{"", "24", "", "", false},
	}
	for _, c := range cases {
		id, status, ok := questStatusFromTemplate(c.templateID, c.rawType)
		if id != c.wantID || status != c.wantStatus || ok != c.wantOK {
			t.Errorf("questStatusFromTemplate(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
				c.templateID, c.rawType, id, status, ok, c.wantID, c.wantStatus, c.wantOK)
		}
	}
}

func TestReadNewLinesWaitsForPartialTailAndHandlesRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application_test.log")
	if err := os.WriteFile(path, []byte("one\npartial"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, position := readNewLines(path, 0)
	if len(lines) != 1 || lines[0] != "one" || position != 4 {
		t.Fatalf("first read: lines=%v position=%d", lines, position)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("-done\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	lines, position = readNewLines(path, position)
	if len(lines) != 1 || lines[0] != "partial-done" {
		t.Fatalf("completed tail: lines=%v position=%d", lines, position)
	}
	if err := os.WriteFile(path, []byte("rotated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, _ = readNewLines(path, position)
	if len(lines) != 1 || lines[0] != "rotated" {
		t.Fatalf("rotated read: %v", lines)
	}
}

func TestStatusToString(t *testing.T) {
	if got := statusToString(float64(24)); got != "24" {
		t.Errorf("number: %q", got)
	}
	if got := statusToString("questComplete"); got != "questComplete" {
		t.Errorf("string: %q", got)
	}
}

func TestProcessLinesKeepsPartialQuestJSONAcrossScans(t *testing.T) {
	a := New(Config{})
	var gotID, gotStatus string
	a.onQuest = func(id, status string) { gotID, gotStatus = id, status }
	a.processLines([]string{
		"2026-08-29 12:00:00.001 INFO " + taskSubstring,
		`{"message":{"type":12,`,
	})
	if gotID != "" {
		t.Fatal("incomplete notification was emitted")
	}
	a.processLines([]string{`"templateId":"task-a successMessageText"}}`})
	if gotID != "task-a" || gotStatus != "completed" {
		t.Fatalf("quest = (%q, %q)", gotID, gotStatus)
	}
}

func TestProcessLinesDetectsProfileAndModeForBuiltInAgent(t *testing.T) {
	a := New(Config{Wipe: "wipe-a"})
	var got progress.Scope
	a.onScope = func(scope progress.Scope) { got = scope }
	a.processLines([]string{
		"2026-08-29 12:00:00.001 INFO Session mode: Pve",
		"2026-08-29 12:00:00.002 INFO SelectProfile ProfileId:profile123 AccountId:456",
	})
	if got.Profile != "profile123" || got.Mode != "pve" || got.Wipe != "wipe-a" {
		t.Fatalf("scope = %#v", got)
	}
}
