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
	"tarkovmap/internal/progress"
	"tarkovmap/internal/registry"
)

const historyLimit = 500

type PositionEvent struct {
	MapID    string      `json:"mapId"`
	World    parser.Vec3 `json:"world"`
	Rotation parser.Quat `json:"rotation"`
	Raw      string      `json:"rawFilename,omitempty"`
	Floor    string      `json:"floor,omitempty"`
	Time     time.Time   `json:"time"`
	Source   string      `json:"source"` // "screenshot" | "manual" | "agent"
}

type Settings struct {
	// CleanScreenshots: delete this session's screenshots (created after the
	// agent started) when entering a new map — TarkovPilot parity (PLAN §5:
	// opt-in switch, never on by default).
	CleanScreenshots bool `json:"cleanScreenshots"`
}

type persisted struct {
	CurrentMap  string                           `json:"currentMap"`
	Position    *PositionEvent                   `json:"position"`
	Projections map[string]registry.Projection   `json:"projections"`
	FloorRanges map[string][]registry.FloorRange `json:"floorRanges"`
	QuestStatus map[string]string                `json:"questStatus,omitempty"`
	Progress    *progress.Book                   `json:"progress,omitempty"`
	ActiveScope progress.Scope                   `json:"activeScope"`
	Settings    Settings                         `json:"settings"`
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
	progress    *progress.Book
	activeScope progress.Scope
	settings    Settings
	onChange    func()
}

func New(path string, reg *registry.Registry) *Store {
	s := &Store{
		path:        path,
		reg:         reg,
		overrides:   map[string]registry.Projection{},
		floorRng:    map[string][]registry.FloorRange{},
		progress:    progress.NewBook(),
		activeScope: progress.NormalizeScope(progress.Scope{}),
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
	state := s.progress.Snapshot(s.activeScope)
	out := make(map[string]string, len(state.Tasks))
	for k, record := range state.Tasks {
		if record.Status != progress.StatusUntracked {
			out[k] = record.Status
		}
	}
	return out
}

// SetQuestStatus records a quest status; empty status clears the record.
func (s *Store) SetQuestStatus(questID, status string) {
	if status == "" {
		status = progress.StatusUntracked
	}
	s.mu.Lock()
	s.progress.SetTask(s.activeScope, questID, status, "manual", time.Now().UTC())
	s.mu.Unlock()
	s.save()
	s.changed()
}

func (s *Store) ActiveScope() progress.Scope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeScope
}

func (s *Store) SetActiveScope(scope progress.Scope) {
	s.mu.Lock()
	s.activeScope = progress.NormalizeScope(scope)
	s.progress.State(s.activeScope)
	s.mu.Unlock()
	s.save()
	s.changed()
}

func (s *Store) Progress(scope progress.Scope) progress.ProfileState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress.Snapshot(scope)
}

func (s *Store) ApplyTaskEvents(events []progress.TaskEvent) []progress.ApplyResult {
	now := time.Now().UTC()
	s.mu.Lock()
	results := make([]progress.ApplyResult, len(events))
	changed := false
	for i, event := range events {
		if event.Profile == "" {
			event.Profile = s.activeScope.Profile
		}
		if event.Mode == "" {
			event.Mode = s.activeScope.Mode
		}
		if event.Wipe == "" {
			event.Wipe = s.activeScope.Wipe
		}
		results[i] = s.progress.ApplyEvent(event, now)
		changed = changed || results[i].Applied
	}
	s.mu.Unlock()
	if changed {
		s.save()
		s.changed()
	}
	return results
}

func (s *Store) PreviewTaskEvents(events []progress.TaskEvent) []progress.ApplyResult {
	s.mu.Lock()
	book := s.progress.Clone()
	scope := s.activeScope
	s.mu.Unlock()
	now := time.Now().UTC()
	results := make([]progress.ApplyResult, len(events))
	for i, event := range events {
		if event.Profile == "" {
			event.Profile = scope.Profile
		}
		if event.Mode == "" {
			event.Mode = scope.Mode
		}
		if event.Wipe == "" {
			event.Wipe = scope.Wipe
		}
		results[i] = book.ApplyEvent(event, now)
	}
	return results
}

func (s *Store) SetObjectiveStatus(scope progress.Scope, objectiveID string, completed bool) progress.ApplyResult {
	s.mu.Lock()
	result := s.progress.SetObjective(scope, objectiveID, completed, time.Now().UTC())
	s.mu.Unlock()
	if result.Applied {
		s.save()
		s.changed()
	}
	return result
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
	migrated := false
	if p.Progress != nil {
		p.Progress.Ensure()
		s.progress = p.Progress
	} else if len(p.QuestStatus) > 0 {
		// Preserve the exact pre-migration state before the first v2 save.
		if _, err := os.Stat(s.path + ".v1.bak"); os.IsNotExist(err) {
			_ = os.WriteFile(s.path+".v1.bak", b, 0o644)
		}
		now := time.Now().UTC()
		for taskID, status := range p.QuestStatus {
			if status == "" {
				continue
			}
			s.progress.SetTask(s.activeScope, taskID, status, "legacy-migration", now)
		}
		migrated = true
	}
	if p.ActiveScope.Profile != "" || p.ActiveScope.Mode != "" || p.ActiveScope.Wipe != "" {
		s.activeScope = progress.NormalizeScope(p.ActiveScope)
	}
	s.settings = p.Settings
	if migrated {
		s.save()
	}
}

func (s *Store) save() {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	var position *PositionEvent
	if s.position != nil {
		copyPosition := *s.position
		position = &copyPosition
	}
	projections := make(map[string]registry.Projection, len(s.overrides))
	for id, projection := range s.overrides {
		projection.HorizontalAxes = append([]string(nil), projection.HorizontalAxes...)
		projections[id] = projection
	}
	floorRanges := make(map[string][]registry.FloorRange, len(s.floorRng))
	for id, ranges := range s.floorRng {
		floorRanges[id] = append([]registry.FloorRange(nil), ranges...)
	}
	p := persisted{
		CurrentMap:  s.current,
		Position:    position,
		Projections: projections,
		FloorRanges: floorRanges,
		Progress:    s.progress.Clone(),
		ActiveScope: s.activeScope,
		Settings:    s.settings,
	}
	s.mu.Unlock()
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}
