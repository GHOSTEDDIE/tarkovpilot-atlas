package autoupdate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckOnceSchedulesSuccessAndPersistsVersions(t *testing.T) {
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	status := CheckOnce(context.Background(), Config{
		Interval: 12 * time.Hour, Now: func() time.Time { return now },
		ContentUpdate:  func(context.Context) (string, error) { return "content-v2", nil },
		MapAssetUpdate: func(context.Context) (string, error) { return "maps-v3", nil },
	}, Status{})
	if status.Checking || status.ContentVersion != "content-v2" || status.MapAssetVersion != "maps-v3" {
		t.Fatalf("status = %#v", status)
	}
	if !status.LastSuccess.Equal(now) || !status.NextCheck.Equal(now.Add(12*time.Hour)) {
		t.Fatalf("schedule = %#v", status)
	}
	path := filepath.Join(t.TempDir(), "update.json")
	if err := Save(path, status); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.ContentVersion != status.ContentVersion || !loaded.NextCheck.Equal(status.NextCheck) {
		t.Fatalf("loaded = %#v, %v", loaded, err)
	}
}

func TestCheckOnceUsesRetryWithoutDiscardingGoodVersion(t *testing.T) {
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	status := CheckOnce(context.Background(), Config{
		RetryInterval: 30 * time.Minute, Now: func() time.Time { return now },
		ContentUpdate:  func(context.Context) (string, error) { return "", errors.New("offline") },
		MapAssetUpdate: func(context.Context) (string, error) { return "maps-v4", nil },
	}, Status{ContentVersion: "content-v1", MapAssetVersion: "maps-v3"})
	if status.ContentVersion != "content-v1" || status.MapAssetVersion != "maps-v4" || status.ContentError != "offline" {
		t.Fatalf("status = %#v", status)
	}
	if !status.NextCheck.Equal(now.Add(30*time.Minute)) || !status.LastSuccess.IsZero() {
		t.Fatalf("retry schedule = %#v", status)
	}
}
