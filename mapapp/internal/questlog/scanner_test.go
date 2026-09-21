package questlog

import (
	"os"
	"path/filepath"
	"testing"

	"tarkovmap/internal/progress"
)

func TestScanFileParsesNumericStatusesAndMultilineJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-notifications_test.log")
	logData := `2026-08-29 12:00:00.001 INFO push-notifications|Got notification | ChatMessageReceived
{
  "message": {
    "type": 10,
    "templateId": "task-a unrelatedSuffix"
  }
}
2026-08-29 12:01:00.001 INFO push-notifications|Got notification | ChatMessageReceived
{
  "message": {"type": 12, "templateId": "task-a successMessageText"}
}
2026-08-29 12:02:00.001 INFO next line
`
	if err := os.WriteFile(path, []byte(logData), 0o644); err != nil {
		t.Fatal(err)
	}
	events, skipped, err := ScanFile(path, progress.Scope{Profile: "p", Mode: "pve", Wipe: "w"})
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(events) != 2 {
		t.Fatalf("events=%d skipped=%d", len(events), skipped)
	}
	if events[0].Status != progress.StatusStarted || events[1].Status != progress.StatusCompleted {
		t.Fatalf("wrong statuses: %#v", events)
	}
	if events[0].EventID == events[1].EventID || events[0].Offset == events[1].Offset {
		t.Fatal("events did not receive stable, distinct idempotency keys")
	}
}

func TestTemplateSuffixIsCompatibilityFallback(t *testing.T) {
	_, status, ok := StatusFromNotification("task-a failMessageText", 24)
	if !ok || status != progress.StatusFailed {
		t.Fatalf("fallback status = %q, ok=%v", status, ok)
	}
	if _, _, ok := StatusFromNotification("task-a unknown", 24); ok {
		t.Fatal("unknown notification type must not become task state")
	}
}

func TestScanDirDiscoversProfileVersionBreakpointsAndSeparatesModes(t *testing.T) {
	root := t.TempDir()
	makeSession := func(name, mode, profile, version, task string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		application := "2026-08-29 12:00:00.001 INFO Session mode: " + mode + "\n" +
			"2026-08-29 12:00:00.002 INFO SelectProfile ProfileId:" + profile + " AccountId:123\n" +
			"2026-08-29 12:00:00.003 INFO application|Init: pstrGameVersion: " + version + "\n"
		if err := os.WriteFile(filepath.Join(dir, "application_test.log"), []byte(application), 0o644); err != nil {
			t.Fatal(err)
		}
		push := "2026-08-29 12:01:00.001 INFO " + Marker + "\n{\"message\":{\"type\":12,\"templateId\":\"" + task + " successMessageText\"}}\n"
		if err := os.WriteFile(filepath.Join(dir, "push-notifications_test.log"), []byte(push), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	makeSession("2026.08.01_00-00-00_1", "Regular", "profilepvp", "1.0.0", "task-pvp")
	makeSession("2026.08.02_00-00-00_2", "Pve", "profilepve", "1.1.0", "task-pve")

	pvp, err := ScanDir(root, progress.Scope{Mode: "pvp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pvp.Breakpoints) != 2 || len(pvp.Events) != 1 || pvp.Events[0].TaskID != "task-pvp" || pvp.ModeSkipped != 1 {
		t.Fatalf("unexpected PvP scan: %#v", pvp)
	}
	pve, err := ScanDirFrom(root, progress.Scope{Mode: "pve"}, "2026.08.02_00-00-00_2", "profilepve")
	if err != nil {
		t.Fatal(err)
	}
	if len(pve.Events) != 1 || pve.Events[0].TaskID != "task-pve" {
		t.Fatalf("unexpected PvE scan: %#v", pve)
	}
}
