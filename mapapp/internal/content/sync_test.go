package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestRefreshValidatesAppliesAndRollsBack(t *testing.T) {
	client := fixtureClient(false)
	target := filepath.Join(t.TempDir(), "content.json")
	firstTime := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	report, err := Refresh(context.Background(), RefreshOptions{
		BaseURL: "https://fixture.invalid", Target: target, Apply: true, Client: client, Now: func() time.Time { return firstTime },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.Modes[ModePVP].Tasks != 11 || report.Modes[ModePVE].Extracts != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	loaded, err := decodePackFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(loaded.Modes[ModePVP].Features); got != 1 {
		t.Fatalf("factory variants were not deduplicated: %d", got)
	}
	if task := findTask(loaded.Modes[ModePVP].Tasks, "storyline:they-are-already-here"); task == nil || len(task.Objectives) < 20 {
		t.Fatalf("storyline overlay was not included in refreshed pack: %#v", task)
	}
	firstVersion := loaded.Version

	secondTime := firstTime.Add(time.Hour)
	if _, err := Refresh(context.Background(), RefreshOptions{
		BaseURL: "https://fixture.invalid", Target: target, Apply: true, Client: fixtureClientNamed(false, "Task A updated"), Now: func() time.Time { return secondTime },
	}); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(target); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := decodePackFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Version != firstVersion {
		t.Fatalf("rollback version = %s, want %s", rolledBack.Version, firstVersion)
	}
}

func TestRefreshDoesNotRewriteUnchangedPack(t *testing.T) {
	client := fixtureClient(false)
	target := filepath.Join(t.TempDir(), "content.json")
	first, err := Refresh(context.Background(), RefreshOptions{
		BaseURL: "https://fixture.invalid", Target: target, Apply: true, Client: client,
		Now: func() time.Time { return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Refresh(context.Background(), RefreshOptions{
		BaseURL: "https://fixture.invalid", Target: target, Apply: true, Client: client,
		Now: func() time.Time { return time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Unchanged || second.Applied || second.Version != first.Version {
		t.Fatalf("unchanged refresh = %#v", second)
	}
	if _, err := os.Stat(target + ".previous"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unchanged refresh created rollback file: %v", err)
	}
}

func findTask(tasks []Task, id string) *Task {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i]
		}
	}
	return nil
}

func TestRefreshRejectsDamagedUpstreamPayload(t *testing.T) {
	_, err := Refresh(context.Background(), RefreshOptions{
		BaseURL: "https://fixture.invalid", Target: filepath.Join(t.TempDir(), "content.json"),
		Client: fixtureClient(true),
	})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error = %v", err)
	}
}

func TestRefreshLeavesTargetUntouchedWhenOffline(t *testing.T) {
	target := filepath.Join(t.TempDir(), "content.json")
	original := []byte("keep-me")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	if _, err := Refresh(context.Background(), RefreshOptions{BaseURL: "https://fixture.invalid", Target: target, Apply: true, Client: client}); err == nil {
		t.Fatal("offline refresh unexpectedly succeeded")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("failed refresh changed the active file")
	}
}

func TestPreserveKnownGoodDataKeepsCoordinatesAndRetiresRemovedData(t *testing.T) {
	previous := &ModeCatalog{Tasks: []Task{
		{ID: "kept", Objectives: []TaskObjective{{
			ID: "objective", ProgressID: "kept:objective", Maps: []string{"customs"},
			Locations: []ObjectiveLocation{{ID: "zone", MapID: "customs", Position: Vec3{X: 1, Y: 2, Z: 3}}},
		}}},
		{ID: "removed", Objectives: []TaskObjective{{
			ID: "old-objective", ProgressID: "removed:old-objective",
			Locations: []ObjectiveLocation{{ID: "old-zone", MapID: "woods", Position: Vec3{X: 4, Y: 5, Z: 6}}},
		}}},
	}}
	current := &ModeCatalog{Tasks: []Task{{
		ID: "kept", Objectives: []TaskObjective{{ID: "objective", ProgressID: "kept:objective"}},
	}}}

	preserved := preserveKnownGoodData(current, previous)
	if preserved.Tasks != 1 || preserved.Objectives != 1 || preserved.Locations != 2 {
		t.Fatalf("preserved = %#v", preserved)
	}
	kept := findTask(current.Tasks, "kept")
	if kept == nil || len(kept.Objectives[0].Locations) != 1 || kept.Objectives[0].Maps[0] != "customs" {
		t.Fatalf("known coordinate was not preserved: %#v", kept)
	}
	if !kept.Objectives[0].Locations[0].Retired {
		t.Fatalf("preserved coordinate must not be exposed as current: %#v", kept.Objectives[0].Locations[0])
	}
	removed := findTask(current.Tasks, "removed")
	if removed == nil || !removed.Retired || !removed.Objectives[0].Retired {
		t.Fatalf("removed upstream data was not retained as retired: %#v", removed)
	}
	if previous.Tasks[1].Objectives[0].Retired {
		t.Fatal("preservation mutated the previous last-known-good catalog")
	}
	activeTasks := NewCatalog(Pack{Modes: map[Mode]*ModeCatalog{ModePVP: current}}, "test").Tasks(ModePVP, "")
	if len(activeTasks) != 1 || len(activeTasks[0].Objectives[0].Locations) != 0 {
		t.Fatalf("retired data leaked into active catalog: %#v", activeTasks)
	}
}

func TestBuildModeDoesNotMergeDuplicateUpstreamFeatureIDs(t *testing.T) {
	up := &upstreamMode{
		Maps: map[string]rawMap{
			"56f40101d2720b2a4d8b45d6": {
				ID: "56f40101d2720b2a4d8b45d6",
				Extracts: []rawExtract{
					{ID: "duplicate", Name: "Gate", Faction: "PMC", Position: Vec3{X: 1, Z: 2}},
					{ID: "duplicate", Name: "Gate", Faction: "Scav", Position: Vec3{X: 3, Z: 4}},
				},
			},
		},
		Tasks: map[string]rawTask{}, Traders: map[string]rawTrader{},
		English: map[string]string{"Gate": "Gate"}, Chinese: map[string]string{"Gate": "大门"},
	}

	catalog, _, _ := buildMode(up)
	if len(catalog.Features) != 2 {
		t.Fatalf("duplicate upstream IDs lost a feature: %#v", catalog.Features)
	}
	if catalog.Features[0].ID == catalog.Features[1].ID {
		t.Fatalf("local feature IDs collided: %#v", catalog.Features)
	}
	for _, feature := range catalog.Features {
		if len(feature.SourceIDs) != 1 || feature.SourceIDs[0] != "duplicate" {
			t.Fatalf("raw source ID was not retained: %#v", feature)
		}
	}
}

func TestStableFeatureIDIgnoresUpstreamIDChanges(t *testing.T) {
	feature := func(id string) rawExtract {
		return rawExtract{ID: id, Name: "Gate", Faction: "PMC", Position: Vec3{X: 12.34, Y: 1.2, Z: -56.78}}
	}
	up := &upstreamMode{
		Maps: map[string]rawMap{
			"55f2d3fd4bdc2d5f408b4567": {ID: "55f2d3fd4bdc2d5f408b4567", Extracts: []rawExtract{feature("old-id")}},
			"59fc81d786f774390775787e": {ID: "59fc81d786f774390775787e", Extracts: []rawExtract{feature("new-id")}},
		},
		Tasks: map[string]rawTask{}, Traders: map[string]rawTrader{},
		English: map[string]string{"Gate": "Gate"}, Chinese: map[string]string{"Gate": "大门"},
	}
	catalog, _, _ := buildMode(up)
	if len(catalog.Features) != 1 || len(catalog.Features[0].SourceIDs) != 2 {
		t.Fatalf("same feature with changed upstream ID did not merge: %#v", catalog.Features)
	}
}

func TestDynamicMapIDResolvesChangedUpstreamObjectID(t *testing.T) {
	const changedID = "changed-lighthouse-object-id"
	up := &upstreamMode{
		Maps: map[string]rawMap{changedID: {ID: changedID, NormalizedName: "lighthouse"}},
		Tasks: map[string]rawTask{"task": {
			ID: "task", Name: "Task", Map: changedID,
			Objectives: []rawObjective{{
				ID: "objective", Description: "Visit", Maps: []string{changedID},
				Zones: []rawZone{{ID: "zone", Map: changedID, Position: Vec3{X: 1, Y: 2, Z: 3}}},
			}},
		}},
		Traders: map[string]rawTrader{}, English: map[string]string{}, Chinese: map[string]string{},
	}
	up.MapIDs = dynamicMapIDs(up.Maps)
	catalog, _, _ := buildMode(up)
	if len(catalog.Tasks) != 1 || catalog.Tasks[0].MapID != "lighthouse" || catalog.Tasks[0].Objectives[0].Locations[0].MapID != "lighthouse" {
		t.Fatalf("dynamic map ID was not resolved: %#v", catalog.Tasks)
	}
}

func fixtureClient(damaged bool) *http.Client {
	return fixtureClientNamed(damaged, "Task A")
}

func fixtureClientNamed(damaged bool, taskName string) *http.Client {
	task := rawTask{
		ID: "task-a", Name: taskName, Trader: "trader-a", Map: "56f40101d2720b2a4d8b45d6",
		Objectives: []rawObjective{{
			ID: "objective-a", Description: "Go there", Type: "visit",
			Zones: []rawZone{{ID: "zone-a", Map: "56f40101d2720b2a4d8b45d6", Position: Vec3{X: 1, Y: 2, Z: 3}}},
		}},
	}
	var tasks rawTasksEnvelope
	tasks.Data.Tasks = map[string]rawTask{task.ID: task}
	featureX := float64(5)
	if taskName != "Task A" {
		featureX = 6
	}
	feature := rawExtract{ID: "extract-a", Name: "Exit A", Faction: "PMC", Position: Vec3{X: featureX, Y: 0, Z: 8}}
	var maps rawMapsEnvelope
	maps.Data.Maps = map[string]rawMap{
		"55f2d3fd4bdc2d5f408b4567": {ID: "55f2d3fd4bdc2d5f408b4567", Extracts: []rawExtract{feature}},
		"59fc81d786f774390775787e": {ID: "59fc81d786f774390775787e", Extracts: []rawExtract{feature}},
	}
	traders := rawTradersEnvelope{Data: map[string]rawTrader{"trader-a": {ID: "trader-a", Name: "Trader A"}}}
	translations := rawTranslationEnvelope{Data: map[string]string{
		taskName: "任务 A", "Go there": "前往目标", "Exit A": "出口 A", "Trader A": "商人 A",
	}}
	bodies := map[string][]byte{}
	for key, value := range map[string]any{"tasks": tasks, "maps": maps, "traders": traders} {
		bodies[key], _ = json.Marshal(value)
	}
	for _, key := range []string{"tasks_en", "tasks_zh", "maps_en", "maps_zh", "traders_en", "traders_zh"} {
		bodies[key], _ = json.Marshal(translations)
	}
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		endpoint := filepath.Base(request.URL.Path)
		body := bodies[endpoint]
		if damaged && endpoint == "tasks" {
			body = []byte("{")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"ETag": []string{"fixture-etag"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    request,
		}, nil
	})}
}
