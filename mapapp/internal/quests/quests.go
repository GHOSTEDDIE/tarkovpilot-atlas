// Quest objective locations, distilled from TarkovTracker/tarkovdata
// (quests.json — objectives carrying inline GPS percent positions).
// Positions are percent offsets on the SVG map artwork, independent of
// the world-coordinate projection used for the player marker.
package quests

import (
	_ "embed"
	"encoding/json"
)

//go:embed quests.json
var questsJSON []byte

type Objective struct {
	Desc   string  `json:"desc,omitempty"`
	DescZh string  `json:"descZh,omitempty"` // official locale text, matched by condition id
	Map    string  `json:"map"`
	Wx     float64 `json:"wx"` // world coordinates (same space as the player position)
	Wy     float64 `json:"wy"` // height — used for floor detection
	Wz     float64 `json:"wz"`
}

type Quest struct {
	ID         string      `json:"id"` // BSG gameId — matches log notification templateIds
	Title      string      `json:"title"`
	TitleZh    string      `json:"titleZh,omitempty"` // official Chinese name (game locale)
	TitleRu    string      `json:"titleRu,omitempty"`
	Giver      string      `json:"giver,omitempty"`
	GiverZh    string      `json:"giverZh,omitempty"`
	Objectives []Objective `json:"objectives"`
}

type Registry struct {
	Quests []Quest
	byID   map[string]*Quest
}

func Load() (*Registry, error) {
	var raw struct {
		Quests []Quest `json:"quests"`
	}
	if err := json.Unmarshal(questsJSON, &raw); err != nil {
		return nil, err
	}
	r := &Registry{Quests: raw.Quests, byID: map[string]*Quest{}}
	for i := range r.Quests {
		r.byID[r.Quests[i].ID] = &r.Quests[i]
	}
	return r, nil
}

func MustLoad() *Registry {
	r, err := Load()
	if err != nil {
		panic(err)
	}
	return r
}

func (r *Registry) Get(id string) *Quest { return r.byID[id] }
