// Package content owns the versioned, offline game-content catalog used by
// the server. Upstream tarkov.dev JSON shapes never escape this package.
package content

import "time"

const SchemaVersion = 1

type Mode string

const (
	ModePVP Mode = "pvp"
	ModePVE Mode = "pve"
)

func NormalizeMode(v string) Mode {
	if Mode(v) == ModePVE {
		return ModePVE
	}
	return ModePVP
}

type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type SourceInfo struct {
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

type Requirement struct {
	TaskID   string   `json:"taskId"`
	Statuses []string `json:"statuses,omitempty"`
}

type ObjectiveLocation struct {
	ID           string   `json:"id"`
	MapID        string   `json:"mapId"`
	SourceMapID  string   `json:"sourceMapId,omitempty"`
	Position     Vec3     `json:"position"`
	Outline      []Vec3   `json:"outline,omitempty"`
	Top          *float64 `json:"top,omitempty"`
	Bottom       *float64 `json:"bottom,omitempty"`
	FloorPending bool     `json:"floorPending,omitempty"`
	Retired      bool     `json:"retired,omitempty"`
}

type ObjectiveMedia struct {
	ObjectiveID string    `json:"objectiveId"`
	MapID       string    `json:"mapId"`
	LocalFile   string    `json:"localFile"`
	CaptionZh   string    `json:"captionZh,omitempty"`
	SourceURL   string    `json:"sourceUrl"`
	Author      string    `json:"author"`
	License     string    `json:"license"`
	GameVersion string    `json:"gameVersion,omitempty"`
	Checksum    string    `json:"checksum"`
	ReviewedAt  time.Time `json:"reviewedAt"`
}

type TaskObjective struct {
	ID            string              `json:"id"`
	ProgressID    string              `json:"progressId"`
	Description   string              `json:"description"`
	DescriptionZh string              `json:"descriptionZh,omitempty"`
	Type          string              `json:"type"`
	Optional      bool                `json:"optional,omitempty"`
	Retired       bool                `json:"retired,omitempty"`
	Maps          []string            `json:"maps,omitempty"`
	Locations     []ObjectiveLocation `json:"locations,omitempty"`
	Media         []ObjectiveMedia    `json:"media,omitempty"`
}

type Task struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	NameZh              string          `json:"nameZh,omitempty"`
	Description         string          `json:"description,omitempty"`
	DescriptionZh       string          `json:"descriptionZh,omitempty"`
	TraderID            string          `json:"traderId,omitempty"`
	Trader              string          `json:"trader,omitempty"`
	TraderZh            string          `json:"traderZh,omitempty"`
	Category            string          `json:"category,omitempty"` // empty/standard | storyline
	StorylineOrder      int             `json:"storylineOrder,omitempty"`
	MapID               string          `json:"mapId,omitempty"`
	MinPlayerLevel      int             `json:"minPlayerLevel,omitempty"`
	Faction             string          `json:"faction,omitempty"`
	KappaRequired       bool            `json:"kappaRequired,omitempty"`
	LightkeeperRequired bool            `json:"lightkeeperRequired,omitempty"`
	Retired             bool            `json:"retired,omitempty"`
	WikiLink            string          `json:"wikiLink,omitempty"`
	TaskImageLink       string          `json:"taskImageLink,omitempty"`
	Requirements        []Requirement   `json:"requirements,omitempty"`
	Objectives          []TaskObjective `json:"objectives,omitempty"`
}

type MapFeature struct {
	ID           string   `json:"id"`
	SourceIDs    []string `json:"sourceIds,omitempty"`
	Kind         string   `json:"kind"` // extract | transit
	MapID        string   `json:"mapId"`
	SourceMapIDs []string `json:"sourceMapIds,omitempty"`
	Name         string   `json:"name"`
	NameZh       string   `json:"nameZh,omitempty"`
	Faction      string   `json:"faction,omitempty"` // pmc | scav | shared
	Destination  string   `json:"destinationMapId,omitempty"`
	Conditions   string   `json:"conditions,omitempty"`
	SwitchIDs    []string `json:"switchIds,omitempty"`
	TransferItem string   `json:"transferItem,omitempty"`
	Position     Vec3     `json:"position"`
	Outline      []Vec3   `json:"outline,omitempty"`
	Top          *float64 `json:"top,omitempty"`
	Bottom       *float64 `json:"bottom,omitempty"`
	FloorPending bool     `json:"floorPending,omitempty"`
}

type MapCoverage struct {
	MapID        string   `json:"mapId"`
	SourceMapIDs []string `json:"sourceMapIds,omitempty"`
	ExtractCount int      `json:"extractCount"`
	TransitCount int      `json:"transitCount"`
	LocatedTasks int      `json:"locatedTasks"`
	Status       string   `json:"status"` // available | missing
	MessageZh    string   `json:"messageZh,omitempty"`
}

type ModeCatalog struct {
	Tasks    []Task                 `json:"tasks"`
	Features []MapFeature           `json:"features"`
	Coverage map[string]MapCoverage `json:"coverage"`
}

type Pack struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Version       string                `json:"version"`
	GeneratedAt   time.Time             `json:"generatedAt"`
	Sources       []SourceInfo          `json:"sources,omitempty"`
	Modes         map[Mode]*ModeCatalog `json:"modes"`
	Media         []ObjectiveMedia      `json:"media,omitempty"`
}

type Meta struct {
	SchemaVersion int                             `json:"schemaVersion"`
	Version       string                          `json:"version"`
	GeneratedAt   time.Time                       `json:"generatedAt"`
	Source        string                          `json:"source"`
	Modes         []Mode                          `json:"modes"`
	Coverage      map[Mode]map[string]MapCoverage `json:"coverage"`
}
