package content

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

// The public tarkov.dev task catalog does not include the 1.0 story chapters.
// Keep that independently reviewed supplement behind the same Task boundary so
// the API, progress store and UI do not need a second tracking implementation.

//go:embed data/storyline.json
var storylineJSON []byte

type storylineOverlay struct {
	Version string       `json:"version"`
	Sources []SourceInfo `json:"sources"`
	Tasks   []Task       `json:"tasks"`
}

func applyStorylineOverlay(pack *Pack) error {
	var overlay storylineOverlay
	if err := json.Unmarshal(storylineJSON, &overlay); err != nil {
		return fmt.Errorf("decode storyline overlay: %w", err)
	}
	if overlay.Version == "" || len(overlay.Tasks) == 0 {
		return fmt.Errorf("storyline overlay is empty")
	}
	for _, mode := range []Mode{ModePVP, ModePVE} {
		catalog := pack.Modes[mode]
		if catalog == nil {
			continue
		}
		seen := make(map[string]bool, len(catalog.Tasks))
		for _, task := range catalog.Tasks {
			seen[task.ID] = true
		}
		for _, sourceTask := range overlay.Tasks {
			if seen[sourceTask.ID] {
				continue
			}
			task := cloneTask(sourceTask)
			for i := range task.Objectives {
				if task.Objectives[i].ProgressID == "" {
					task.Objectives[i].ProgressID = task.ID + ":" + task.Objectives[i].ID
				}
			}
			catalog.Tasks = append(catalog.Tasks, task)
			seen[task.ID] = true
		}
		sort.SliceStable(catalog.Tasks, func(i, j int) bool {
			a, b := catalog.Tasks[i], catalog.Tasks[j]
			if a.Category == "storyline" && b.Category == "storyline" {
				return a.StorylineOrder < b.StorylineOrder
			}
			if a.Category == "storyline" {
				return true
			}
			if b.Category == "storyline" {
				return false
			}
			return a.ID < b.ID
		})
	}
	for _, source := range overlay.Sources {
		if !hasSource(pack.Sources, source.URL) {
			pack.Sources = append(pack.Sources, source)
		}
	}
	return nil
}

func cloneTask(task Task) Task {
	clone := task
	clone.Requirements = append([]Requirement(nil), task.Requirements...)
	clone.Objectives = append([]TaskObjective(nil), task.Objectives...)
	for i := range clone.Objectives {
		clone.Objectives[i].Maps = append([]string(nil), task.Objectives[i].Maps...)
		sort.Strings(clone.Objectives[i].Maps)
		clone.Objectives[i].Locations = append([]ObjectiveLocation(nil), task.Objectives[i].Locations...)
		clone.Objectives[i].Media = append([]ObjectiveMedia(nil), task.Objectives[i].Media...)
	}
	return clone
}

func hasSource(sources []SourceInfo, url string) bool {
	for _, source := range sources {
		if source.URL == url {
			return true
		}
	}
	return false
}
