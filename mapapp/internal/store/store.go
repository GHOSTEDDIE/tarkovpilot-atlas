// Shared session state: current map, latest position, position history,
// and per-map projection overrides. Persisted to a JSON file.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tarkovmap/internal/parser"
	"tarkovmap/internal/registry"
)

const historyLimit = 500

type PositionEvent struct {
	MapID    string        `json:"mapId"`
	World    parser.Vec3   `json:"world"`
	Rotation parser.Quat   `json:"rotation"`
	Raw      string        `json:"rawFilename,omitempty"`
	Floor    string        `json:"floor,omitempty"`
	Time     time.Time     `json:"time"`
	Source   string        `json:"source"` // "screenshot" | "manual" | "agent"
}

type Settings struct {
	// CleanScreenshots: delete this session's screenshots (created after the
	// agent started) when entering a new map — TarkovPilot parity (PLAN §5:
	// opt-in switch, never on by default).
	CleanScreenshots bool `json:"cleanScreenshots"`
}

type persisted struct {
	CurrentMap  string                       `json:"currentMap"`
	Position    *PositionEvent               `json:"position"`
	Projections map[string]registry.Projection `json:"projections"`
	FloorRanges map[string][]registry.FloorRange `json:"floorRanges"`
	QuestStatus map[string]string            `json:"questStatus"`
	Settings    Settings                     `json:"settings"`
}

type Store struct {
	mu          sync.Mutex
	path        string
	reg         *registry.Registry
	current     string
	position    *PositionEvent
	history     []PositionEvent
	overrides   map[string]registry.Projection
	floorRng    map[string][]registry.FloorRange
	questStatus map[string]string
	settings    Settings
	onChange    func()
}

func New(path string, reg *registry.Registry) *Store {
	s := &Store{
		path:        path,
		reg:         reg,
		overrides:   map[string]registry.Projection{},
		floorRng:    map[string][]registry.FloorRange{},
		questStatus: map[string]string{},
	}
	s.load()
	return s
}

func (s *Store) SetOnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

func (s *Store) changed() {
	s.mu.Lock()
	fn := s.onChange
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *Store) CurrentMap() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *Store) SetMap(id string) bool {
	if s.reg.Get(id) == nil {
		return false
	}
	s.mu.Lock()
	s.current = id
	s.mu.Unlock()
	s.save()
	s.changed()
	return true
}

func (s *Store) Position() *PositionEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.position == nil {
		return nil
	}
	cp := *s.position
	return &cp
}

func (s *Store) History() []PositionEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PositionEvent, len(s.history))
	copy(out, s.history)
	return out
}

func (s *Store) AddPosition(ev PositionEvent) {
	if ev.MapID == "" {
		ev.MapID = s.CurrentMap()
	}
	if m := s.reg.Get(ev.MapID); m != nil {
		if f := m.FloorForY(ev.World.Y); f != "" {
			ev.Floor = f
		}
	}
	ev.Time = time.Now().UTC()
	s.mu.Lock()
	s.position = &ev
	s.history = append(s.history, ev)
	if len(s.history) > historyLimit {
		s.history = s.history[len(s.history)-historyLimit:]
	}
	s.mu.Unlock()
	s.save()
	s.changed()
}

// Maps returns the registry with projection/floor overrides applied.
func (s *Store) Maps() map[string]*registry.Map {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]*registry.Map{}
	for id, m := range s.reg.Maps {
		c := m.Clone()
		if p, ok := s.overrides[id]; ok {
			c.Projection = p
		}
		if fr, ok := s.floorRng[id]; ok {
			c.FloorRanges = fr
		}
		out[id] = c
	}
	return out
}

func (s *Store) SetProjection(mapID string, p registry.Projection) bool {
	if s.reg.Get(mapID) == nil {
		return false
	}
	s.mu.Lock()
	s.overrides[mapID] = p
	s.mu.Unlock()
	s.save()
	s.changed()
	return true
}

func (s *Store) SetFloorRanges(mapID string, fr []registry.FloorRange) bool {
	if s.reg.Get(mapID) == nil {
		return false
	}
	s.mu.Lock()
	s.floorRng[mapID] = fr
	s.mu.Unlock()
	s.save()
	s.changed()
	return true
}

// QuestStatuses returns quest gameId -> status ("" means not tracked).
func (s *Store) QuestStatuses() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.questStatus))
	for k, v := range s.questStatus {
		out[k] = v
	}
	return out
}

// SetQuestStatus records a quest status; empty status clears the record.
func (s *Store) SetQuestStatus(questID, status string) {
	s.mu.Lock()
	if status == "" {
		delete(s.questStatus, questID)
	} else {
		s.questStatus[questID] = status
	}
	s.mu.Unlock()
	s.save()
	s.changed()
}

func (s *Store) GetSettings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

func (s *Store) SetSettings(v Settings) {
	s.mu.Lock()
	s.settings = v
	s.mu.Unlock()
	s.save()
	s.changed()
}

func (s *Store) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var p persisted
	if json.Unmarshal(b, &p) != nil {
		return
	}
	s.current = p.CurrentMap
	s.position = p.Position
	if p.Projections != nil {
		s.overrides = p.Projections
	}
	if p.FloorRanges != nil {
		s.floorRng = p.FloorRanges
	}
	if p.QuestStatus != nil {
		s.questStatus = p.QuestStatus
	}
	s.settings = p.Settings
}

func (s *Store) save() {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	p := persisted{
		CurrentMap:  s.current,
		Position:    s.position,
		Projections: s.overrides,
		FloorRanges: s.floorRng,
		QuestStatus: s.questStatus,
		Settings:    s.settings,
	}
	s.mu.Unlock()
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
}
