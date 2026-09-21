package content_test

import (
	"math"
	"testing"

	"tarkovmap/internal/content"
	"tarkovmap/internal/quests"
)

func TestEmbeddedCatalogPreservesLegacyLocatedObjectives(t *testing.T) {
	catalog, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	legacy := quests.MustLoad()
	if got := len(legacy.Quests); got != 173 {
		t.Fatalf("legacy task baseline changed: got %d, want 173", got)
	}
	total := 0
	for _, oldTask := range legacy.Quests {
		task := catalog.Task(content.ModePVP, oldTask.ID)
		if task == nil {
			t.Errorf("legacy task %s missing", oldTask.ID)
			continue
		}
		for _, oldObjective := range oldTask.Objectives {
			total++
			found := false
			for _, objective := range task.Objectives {
				for _, location := range objective.Locations {
					if location.MapID == oldObjective.Map && near(location.Position.X, oldObjective.Wx) &&
						near(location.Position.Y, oldObjective.Wy) && near(location.Position.Z, oldObjective.Wz) {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("legacy coordinate missing: task=%s map=%s xyz=(%.2f,%.2f,%.2f)",
					oldTask.ID, oldObjective.Map, oldObjective.Wx, oldObjective.Wy, oldObjective.Wz)
			}
		}
	}
	if total != 467 {
		t.Fatalf("legacy objective baseline changed: got %d, want 467", total)
	}
}

func TestEmbeddedCatalogIncludesVariantsAndMissingCoverage(t *testing.T) {
	catalog, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	factory := catalog.Coverage(content.ModePVP, "factory")
	if len(factory.SourceMapIDs) != 2 || factory.ExtractCount == 0 {
		t.Fatalf("factory variants were not merged: %#v", factory)
	}
	groundZero := catalog.Coverage(content.ModePVP, "groundzero")
	if len(groundZero.SourceMapIDs) != 3 || groundZero.ExtractCount == 0 {
		t.Fatalf("ground zero variants were not merged: %#v", groundZero)
	}
	lab := catalog.Coverage(content.ModePVP, "lab")
	if len(lab.SourceMapIDs) != 2 || lab.ExtractCount == 0 {
		t.Fatalf("lab variants were not merged: %#v", lab)
	}
	terminal := catalog.Coverage(content.ModePVP, "terminal")
	if terminal.Status != "missing" || terminal.MessageZh == "" {
		t.Fatalf("terminal must expose the upstream data gap: %#v", terminal)
	}
}

func TestLatestMapVariantsResolveToSupportedMaps(t *testing.T) {
	for upstreamID, want := range map[string]string{
		"68236e8153654e8c1200798a": "groundzero",
		"6a294a5b5eb5f9a1700417b7": "lab",
		"6733700029c367a3d40b02af": "labyrinth",
		"69af492a4819ea4ba10a69c5": "icebreaker",
	} {
		got, ok := content.InternalMapID(upstreamID)
		if !ok || got != want {
			t.Fatalf("InternalMapID(%q) = %q, %v; want %q, true", upstreamID, got, ok, want)
		}
	}
}

func TestEmbeddedCatalogIncludesStorylineChapters(t *testing.T) {
	catalog, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []content.Mode{content.ModePVP, content.ModePVE} {
		storylineCount := 0
		for _, task := range catalog.Tasks(mode, "") {
			if task.Category == "storyline" {
				storylineCount++
			}
		}
		if storylineCount != 10 {
			t.Fatalf("%s storyline chapter count = %d, want 10", mode, storylineCount)
		}
		task := catalog.Task(mode, "storyline:they-are-already-here")
		if task == nil {
			t.Fatalf("%s storyline chapter They Are Already Here is missing", mode)
		}
		if task.NameZh != "他们已经来了" {
			t.Fatalf("%s storyline chapter has wrong Chinese name %q", mode, task.NameZh)
		}
		if len(task.Objectives) < 20 {
			t.Fatalf("%s storyline chapter objectives are incomplete: %d", mode, len(task.Objectives))
		}
		if !hasTask(catalog.Tasks(mode, "streetsoftarkov"), task.ID) || !hasTask(catalog.Tasks(mode, "interchange"), task.ID) {
			t.Fatalf("%s storyline chapter is missing map associations", mode)
		}
	}
}

func hasTask(tasks []content.Task, id string) bool {
	for _, task := range tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.011 }
