package server

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tarkovmap/internal/content"
	"tarkovmap/internal/progress"
	"tarkovmap/internal/quests"
	"tarkovmap/internal/registry"
	"tarkovmap/internal/store"
)

func TestCatalogAndProgressAPIs(t *testing.T) {
	reg := registry.MustLoad()
	catalog, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	state := store.New(filepath.Join(t.TempDir(), "state.json"), reg)
	server := New(state, reg, quests.MustLoad(), Config{Catalog: catalog})

	features := requestJSON(t, server.Handler(), http.MethodGet, "/api/maps/customs/features?mode=pvp", nil)
	if got := len(features["features"].([]any)); got == 0 {
		t.Fatal("customs features missing")
	}
	tasks := requestJSON(t, server.Handler(), http.MethodGet, "/api/tasks?mode=pvp", nil)
	if !containsTask(tasks["tasks"].([]any), "storyline:they-are-already-here") {
		t.Fatal("storyline chapter They Are Already Here missing from task API")
	}
	if containsTask(tasks["tasks"].([]any), "669fa394e0c9f9fafa082897") {
		t.Fatal("retired event task leaked into active task API")
	}
	labyrinth := requestJSON(t, server.Handler(), http.MethodGet, "/api/maps/labyrinth/features?mode=pvp", nil)
	if got := len(labyrinth["features"].([]any)); got != 2 {
		t.Fatalf("labyrinth features = %d, want 2", got)
	}
	icebreaker := requestJSON(t, server.Handler(), http.MethodGet, "/api/maps/icebreaker/features?mode=pvp", nil)
	if got := len(icebreaker["tasks"].([]any)); got == 0 {
		t.Fatal("icebreaker tasks missing")
	}

	requestJSON(t, server.Handler(), http.MethodPut, "/api/progress/scope", map[string]any{
		"profile": "profile-a", "mode": "pve", "wipe": "wipe-a",
	})
	requestJSON(t, server.Handler(), http.MethodPut, "/api/progress/tasks/task-a", map[string]any{"status": "started"})
	requestJSON(t, server.Handler(), http.MethodPut, "/api/progress/objectives/task-a%3Aobjective-a", map[string]any{"completed": true})

	snapshot := state.Progress(progress.Scope{Profile: "profile-a", Mode: "pve", Wipe: "wipe-a"})
	if snapshot.Tasks["task-a"].Status != progress.StatusStarted {
		t.Fatalf("task status = %q", snapshot.Tasks["task-a"].Status)
	}
	if snapshot.Objectives["task-a:objective-a"].Status != progress.StatusCompleted {
		t.Fatalf("objective status = %q", snapshot.Objectives["task-a:objective-a"].Status)
	}

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []progress.TaskEvent{
		{EventID: "complete", Profile: "profile-a", Mode: "pve", Wipe: "wipe-a", TaskID: "task-b", Status: "completed", OccurredAt: t1},
		{EventID: "late-fail", Profile: "profile-a", Mode: "pve", Wipe: "wipe-a", TaskID: "task-b", Status: "failed", OccurredAt: t1.Add(time.Hour)},
	}
	requestJSON(t, server.Handler(), http.MethodPost, "/api/ingest/quest-events", map[string]any{"events": events})
	if got := state.Progress(progress.Scope{Profile: "profile-a", Mode: "pve", Wipe: "wipe-a"}).Tasks["task-b"].Status; got != "completed" {
		t.Fatalf("completed task was downgraded to %q", got)
	}
}

func TestBundledLatestMapTilesAreComplete(t *testing.T) {
	reg := registry.MustLoad()
	want := 0
	for _, mapID := range []string{"labyrinth", "icebreaker"} {
		m := reg.Get(mapID)
		for _, layer := range m.Raster.Layers {
			for x := 0; x < 1<<m.Raster.Zoom; x++ {
				for y := 0; y < 1<<m.Raster.Zoom; y++ {
					path := strings.NewReplacer(
						"{z}", strconv.Itoa(m.Raster.Zoom),
						"{x}", strconv.Itoa(x),
						"{y}", strconv.Itoa(y),
					).Replace("maps/" + layer.TilePath)
					info, err := fs.Stat(mapsFS, path)
					if err != nil || info.Size() == 0 {
						t.Fatalf("missing bundled tile %s: %v", path, err)
					}
					file, err := mapsFS.Open(path)
					if err != nil {
						t.Fatal(err)
					}
					config, _, decodeErr := image.DecodeConfig(file)
					_ = file.Close()
					if decodeErr != nil || config.Width != 256 || config.Height != 256 {
						t.Fatalf("invalid bundled tile %s: %dx%d, %v", path, config.Width, config.Height, decodeErr)
					}
					want++
				}
			}
		}
	}
	if want != 272 {
		t.Fatalf("checked %d tiles, want 272", want)
	}
}

func TestBundledLighthouseFallbackImage(t *testing.T) {
	reg := registry.MustLoad()
	m := reg.Get("lighthouse")
	file, err := mapsFS.Open("maps/" + m.FallbackImage)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(file)
	_ = file.Close()
	if err != nil || format != "jpeg" {
		t.Fatalf("decode lighthouse fallback: format=%q err=%v", format, err)
	}
	if config.Width != m.FallbackImageWidth || config.Height != m.FallbackImageHeight {
		t.Fatalf("lighthouse fallback = %dx%d, want %dx%d", config.Width, config.Height, m.FallbackImageWidth, m.FallbackImageHeight)
	}

	catalog, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	state := store.New(filepath.Join(t.TempDir(), "state.json"), reg)
	server := New(state, reg, quests.MustLoad(), Config{Catalog: catalog})
	request := httptest.NewRequest(http.MethodGet, "/maps/"+m.FallbackImage, nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("fallback response: status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func TestBundledSVGsContainEveryRegisteredFloor(t *testing.T) {
	reg := registry.MustLoad()
	for mapID, m := range reg.Maps {
		if m.SvgFile == "" {
			continue
		}
		body, err := fs.ReadFile(mapsFS, "maps/"+m.SvgFile)
		if err != nil {
			t.Fatalf("%s SVG: %v", mapID, err)
		}
		for _, floor := range m.Floors {
			if !bytes.Contains(body, []byte(`id="`+floor+`"`)) && !bytes.Contains(body, []byte(`id='`+floor+`'`)) {
				t.Fatalf("%s SVG is missing registered floor %s", mapID, floor)
			}
		}
	}
}

func TestRuntimeMapPackageOverridesEmbeddedAssetsWithFallback(t *testing.T) {
	reg := registry.MustLoad()
	catalog, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	mapDir := t.TempDir()
	updated := []byte(`<svg viewBox="0 0 1 1"><g id="Ground_Level" data-runtime="true"/></svg>`)
	if err := os.WriteFile(filepath.Join(mapDir, "Lighthouse.svg"), updated, 0o644); err != nil {
		t.Fatal(err)
	}
	state := store.New(filepath.Join(t.TempDir(), "state.json"), reg)
	server := New(state, reg, quests.MustLoad(), Config{
		Catalog: catalog, MapDir: mapDir, MapAssetVersion: "runtime-v1",
	})

	request := httptest.NewRequest(http.MethodGet, "/maps/Lighthouse.svg", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), updated) {
		t.Fatalf("runtime map response: status=%d body=%q", recorder.Code, recorder.Body.Bytes())
	}
	request = httptest.NewRequest(http.MethodGet, "/maps/Factory.svg", nil)
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("<svg")) {
		t.Fatalf("embedded fallback response: status=%d", recorder.Code)
	}
	stateResponse := requestJSON(t, server.Handler(), http.MethodGet, "/api/state", nil)
	if stateResponse["mapAssetVersion"] != "runtime-v1" || stateResponse["catalogVersion"] == "" {
		t.Fatalf("resource versions missing from state: %#v", stateResponse)
	}
}

func containsTask(tasks []any, id string) bool {
	for _, raw := range tasks {
		if task, ok := raw.(map[string]any); ok && task["id"] == id {
			return true
		}
	}
	return false
}

func requestJSON(t *testing.T, handler http.Handler, method, target string, body any) map[string]any {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("%s %s: status=%d body=%s", method, target, recorder.Code, recorder.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
