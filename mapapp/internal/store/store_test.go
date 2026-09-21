package store

import (
	"os"
	"path/filepath"
	"testing"

	"tarkovmap/internal/registry"
)

func TestLegacyQuestStatusMigrationCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapapp-data.json")
	legacy := []byte(`{"currentMap":"customs","questStatus":{"task-a":"completed"}}`)
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(path, registry.MustLoad())
	if got := store.QuestStatuses()["task-a"]; got != "completed" {
		t.Fatalf("migrated status = %q", got)
	}
	backup, err := os.ReadFile(path + ".v1.bak")
	if err != nil {
		t.Fatal("migration backup missing:", err)
	}
	if string(backup) != string(legacy) {
		t.Fatal("migration backup does not preserve the legacy file")
	}
	reloaded := New(path, registry.MustLoad())
	if got := reloaded.QuestStatuses()["task-a"]; got != "completed" {
		t.Fatalf("persisted migrated status = %q", got)
	}
}
