// HTTP server: REST ingest API + SSE event stream + embedded web UI.
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tarkovmap/internal/autoupdate"
	"tarkovmap/internal/content"
	"tarkovmap/internal/gamelogs"
	"tarkovmap/internal/parser"
	"tarkovmap/internal/progress"
	"tarkovmap/internal/questlog"
	"tarkovmap/internal/quests"
	"tarkovmap/internal/registry"
	"tarkovmap/internal/store"
)

//go:embed static
var staticFS embed.FS

//go:embed maps
var mapsFS embed.FS

// DefaultSVGBaseURL — map SVGs are embedded in the binary and served
// locally (no network needed). Override with -svg-base to use a mirror.
const DefaultSVGBaseURL = "/maps/"

type Server struct {
	st           *store.Store
	reg          *registry.Registry
	qr           *quests.Registry
	catalog      *content.Catalog
	token        string // optional ingest token; empty = open
	svgBase      string
	logsDir      string
	mux          *http.ServeMux
	embeddedMaps http.Handler

	resourceMu      sync.RWMutex
	mapDir          string
	mapAssetVersion string
	updateStatus    autoupdate.Status

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

type Config struct {
	Token           string // if set, ingest POSTs need X-Token or ?token=
	SVGBaseURL      string // where the web UI fetches map SVGs from
	Catalog         *content.Catalog
	LogsDir         string
	MapDir          string // validated runtime map package; embedded maps remain fallback
	MapAssetVersion string
	UpdateStatus    autoupdate.Status
}

func New(st *store.Store, reg *registry.Registry, qr *quests.Registry, cfg Config) *Server {
	s := &Server{
		st:              st,
		reg:             reg,
		qr:              qr,
		catalog:         cfg.Catalog,
		token:           cfg.Token,
		svgBase:         cfg.SVGBaseURL,
		logsDir:         cfg.LogsDir,
		mapDir:          cfg.MapDir,
		mapAssetVersion: cfg.MapAssetVersion,
		updateStatus:    cfg.UpdateStatus,
		clients:         map[chan []byte]struct{}{},
	}
	if s.catalog == nil {
		s.catalog, _ = content.Load("")
	}
	if s.svgBase == "" {
		s.svgBase = DefaultSVGBaseURL
	}
	if !strings.HasSuffix(s.svgBase, "/") {
		s.svgBase += "/"
	}

	st.SetOnChange(s.broadcastState)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/quests", s.handleQuests)
	mux.HandleFunc("GET /api/catalog/meta", s.handleCatalogMeta)
	mux.HandleFunc("GET /api/tasks", s.handleTasks)
	mux.HandleFunc("GET /api/maps/{id}/features", s.handleMapFeatures)
	mux.HandleFunc("GET /api/progress", s.handleProgress)
	mux.HandleFunc("PUT /api/progress/scope", s.withAuth(s.handlePutProgressScope))
	mux.HandleFunc("PUT /api/progress/tasks/{id}", s.withAuth(s.handlePutTaskProgress))
	mux.HandleFunc("PUT /api/progress/objectives/{id}", s.withAuth(s.handlePutObjectiveProgress))
	mux.HandleFunc("POST /api/ingest/quest-events", s.withAuth(s.handleIngestQuestEvents))
	mux.HandleFunc("GET /api/log-replay/candidates", s.handleLogReplayCandidates)
	mux.HandleFunc("POST /api/log-replay", s.withAuth(s.handleLogReplay))
	mux.HandleFunc("GET /api/config", s.handleGetConfig) // agents poll this
	mux.HandleFunc("POST /api/config", s.withAuth(s.handleSetConfig))
	mux.HandleFunc("POST /api/ingest/screenshot", s.withAuth(s.handleIngestScreenshot))
	mux.HandleFunc("POST /api/ingest/position", s.withAuth(s.handleIngestPosition))
	mux.HandleFunc("POST /api/ingest/map", s.withAuth(s.handleIngestMap))
	mux.HandleFunc("POST /api/ingest/quest", s.withAuth(s.handleIngestQuest))
	mux.HandleFunc("POST /api/quests/{id}/status", s.withAuth(s.handleSetQuestStatus))
	mux.HandleFunc("POST /api/maps/{id}/projection", s.withAuth(s.handleSetProjection))
	mux.HandleFunc("POST /api/maps/{id}/floors", s.withAuth(s.handleSetFloors))

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	// Map SVGs live outside the frontend build dir (vite empties it on build)
	mapsSub, err := fs.Sub(mapsFS, "maps")
	if err != nil {
		panic(err)
	}
	s.embeddedMaps = http.FileServer(http.FS(mapsSub))
	mux.Handle("GET /maps/", http.StripPrefix("/maps/", http.HandlerFunc(s.handleMapAsset)))
	mux.Handle("GET /media/", http.StripPrefix("/media/", http.FileServer(http.FS(content.MediaFS()))))

	s.mux = mux
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) currentCatalog() *content.Catalog {
	s.resourceMu.RLock()
	defer s.resourceMu.RUnlock()
	return s.catalog
}

func (s *Server) SetCatalog(catalog *content.Catalog) {
	if catalog == nil {
		return
	}
	s.resourceMu.Lock()
	s.catalog = catalog
	s.resourceMu.Unlock()
	s.broadcastState()
}

func (s *Server) SetMapAssets(directory, version string) {
	s.resourceMu.Lock()
	s.mapDir = directory
	s.mapAssetVersion = version
	s.resourceMu.Unlock()
	s.broadcastState()
}

func (s *Server) SetUpdateStatus(status autoupdate.Status) {
	s.resourceMu.Lock()
	s.updateStatus = status
	s.resourceMu.Unlock()
	s.broadcastState()
}

func (s *Server) handleMapAsset(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean("/" + r.URL.Path)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") {
		http.NotFound(w, r)
		return
	}
	s.resourceMu.RLock()
	directory := s.mapDir
	s.resourceMu.RUnlock()
	if directory != "" {
		target := filepath.Join(directory, filepath.FromSlash(clean))
		relative, err := filepath.Rel(directory, target)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
				http.ServeFile(w, r, target)
				return
			}
		}
	}
	s.embeddedMaps.ServeHTTP(w, r)
}

// --- auth ---

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			t := r.Header.Get("X-Token")
			if t == "" {
				t = r.URL.Query().Get("token")
			}
			if t != s.token {
				writeError(w, http.StatusUnauthorized, "bad token")
				return
			}
		}
		next(w, r)
	}
}

// --- state ---

type stateResponse struct {
	MapID           string                   `json:"mapId"`
	Maps            map[string]*registry.Map `json:"maps"`
	Position        *store.PositionEvent     `json:"position"`
	History         []store.PositionEvent    `json:"history"`
	QuestStatus     map[string]string        `json:"questStatus"`
	ActiveScope     progress.Scope           `json:"activeScope"`
	Progress        progress.ProfileState    `json:"progress"`
	Settings        store.Settings           `json:"settings"`
	SVGBaseURL      string                   `json:"svgBaseUrl"`
	CatalogVersion  string                   `json:"catalogVersion"`
	MapAssetVersion string                   `json:"mapAssetVersion"`
	Update          autoupdate.Status        `json:"update"`
	ServerTime      time.Time                `json:"serverTime"`
}

func (s *Server) snapshot() stateResponse {
	scope := s.st.ActiveScope()
	s.resourceMu.RLock()
	catalogVersion := s.catalog.Meta().Version
	mapAssetVersion := s.mapAssetVersion
	updateStatus := s.updateStatus
	s.resourceMu.RUnlock()
	return stateResponse{
		MapID:           s.st.CurrentMap(),
		Maps:            s.st.Maps(),
		Position:        s.st.Position(),
		History:         s.st.History(),
		QuestStatus:     s.st.QuestStatuses(),
		ActiveScope:     scope,
		Progress:        s.st.Progress(scope),
		Settings:        s.st.GetSettings(),
		SVGBaseURL:      s.svgBase,
		CatalogVersion:  catalogVersion,
		MapAssetVersion: mapAssetVersion,
		Update:          updateStatus,
		ServerTime:      time.Now().UTC(),
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snapshot())
}

func (s *Server) broadcastState() {
	b, err := json.Marshal(map[string]any{"type": "state", "state": s.snapshot()})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- b:
		default: // slow client — drop the frame, state is idempotent
		}
	}
}

// --- SSE ---

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 16)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	// initial snapshot so late joiners don't wait for the next change
	b, _ := json.Marshal(map[string]any{"type": "state", "state": s.snapshot()})
	_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			_, _ = w.Write([]byte("data: " + string(msg) + "\n\n"))
			flusher.Flush()
		case <-keepalive.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

// --- ingest ---

type ingestScreenshotReq struct {
	Filename string `json:"filename"`
	MapID    string `json:"mapId,omitempty"`
}

func (s *Server) handleIngestScreenshot(w http.ResponseWriter, r *http.Request) {
	var req ingestScreenshotReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pos, err := parser.Parse(req.Filename)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	mapID := req.MapID
	if mapID == "" {
		mapID = s.st.CurrentMap()
	}
	axisOrder := "x,y,z"
	if m := s.reg.Get(mapID); m != nil {
		if p := s.st.Maps()[mapID]; p != nil {
			axisOrder = p.Projection.AxisOrder
		}
	}
	if err := pos.Remap(axisOrder); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ev := store.PositionEvent{
		MapID:    mapID,
		World:    pos.World,
		Rotation: pos.Rotation,
		Raw:      pos.Raw,
		Source:   "screenshot",
	}
	s.st.AddPosition(ev)
	log.Printf("position: map=%s x=%.2f y=%.2f z=%.2f (%s)", ev.MapID, ev.World.X, ev.World.Y, ev.World.Z, pos.Raw)
	writeJSON(w, map[string]any{"ok": true, "position": s.st.Position()})
}

type ingestPositionReq struct {
	MapID    string      `json:"mapId,omitempty"`
	World    parser.Vec3 `json:"world"`
	Rotation parser.Quat `json:"rotation,omitempty"`
}

func (s *Server) handleIngestPosition(w http.ResponseWriter, r *http.Request) {
	var req ingestPositionReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.st.AddPosition(store.PositionEvent{
		MapID:    req.MapID,
		World:    req.World,
		Rotation: req.Rotation,
		Source:   "manual",
	})
	writeJSON(w, map[string]any{"ok": true, "position": s.st.Position()})
}

type ingestMapReq struct {
	Map string `json:"map"` // raw log name or registry map ID
}

func (s *Server) handleIngestMap(w http.ResponseWriter, r *http.Request) {
	var req ingestMapReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, ok := gamelogs.Resolve(req.Map)
	if !ok {
		if s.reg.Get(strings.ToLower(req.Map)) != nil {
			id, ok = strings.ToLower(req.Map), true
		}
	}
	if !ok {
		// Unknown preset: do NOT switch maps (PLAN §2.3), but report it.
		log.Printf("unknown map preset %q — ignored", req.Map)
		writeJSON(w, map[string]any{"ok": false, "reason": "unknown map preset", "map": req.Map})
		return
	}
	s.st.SetMap(id)
	log.Printf("map: %s (from %q)", id, req.Map)
	writeJSON(w, map[string]any{"ok": true, "mapId": id})
}

// --- settings ---

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.st.GetSettings())
}

func (s *Server) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	var v store.Settings
	if err := decode(r, &v); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.st.SetSettings(v)
	log.Printf("settings: cleanScreenshots=%v", v.CleanScreenshots)
	writeJSON(w, map[string]any{"ok": true})
}

// --- quests ---

func (s *Server) handleQuests(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"quests": s.qr.Quests})
}

func (s *Server) handleCatalogMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.currentCatalog().Meta())
}

func requestMode(r *http.Request) content.Mode {
	return content.NormalizeMode(r.URL.Query().Get("mode"))
}

func requestScope(r *http.Request, fallback progress.Scope) progress.Scope {
	scope := progress.Scope{
		Profile: r.URL.Query().Get("profile"),
		Mode:    r.URL.Query().Get("mode"),
		Wipe:    r.URL.Query().Get("wipe"),
	}
	if scope.Profile == "" {
		scope.Profile = fallback.Profile
	}
	if scope.Mode == "" {
		scope.Mode = fallback.Mode
	}
	if scope.Wipe == "" {
		scope.Wipe = fallback.Wipe
	}
	return progress.NormalizeScope(scope)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	mode := requestMode(r)
	catalog := s.currentCatalog()
	writeJSON(w, map[string]any{
		"mode":  mode,
		"tasks": catalog.Tasks(mode, r.URL.Query().Get("mapId")),
	})
}

func (s *Server) handleMapFeatures(w http.ResponseWriter, r *http.Request) {
	mapID := r.PathValue("id")
	if s.reg.Get(mapID) == nil {
		writeError(w, http.StatusNotFound, "unknown map")
		return
	}
	mode := requestMode(r)
	catalog := s.currentCatalog()
	writeJSON(w, map[string]any{
		"mode": mode, "mapId": mapID,
		"coverage": catalog.Coverage(mode, mapID),
		"features": catalog.Features(mode, mapID),
		"tasks":    catalog.Tasks(mode, mapID),
	})
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	scope := requestScope(r, s.st.ActiveScope())
	writeJSON(w, s.st.Progress(scope))
}

func (s *Server) handlePutProgressScope(w http.ResponseWriter, r *http.Request) {
	var scope progress.Scope
	if err := decode(r, &scope); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.st.SetActiveScope(scope)
	writeJSON(w, map[string]any{"ok": true, "scope": s.st.ActiveScope()})
}

func (s *Server) handlePutTaskProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Status  string `json:"status"`
		Profile string `json:"profile"`
		Mode    string `json:"mode"`
		Wipe    string `json:"wipe"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Status == "" {
		req.Status = progress.StatusUntracked
	}
	if !progress.ValidTaskStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "invalid task status")
		return
	}
	scope := s.st.ActiveScope()
	if req.Profile != "" {
		scope.Profile = req.Profile
	}
	if req.Mode != "" {
		scope.Mode = req.Mode
	}
	if req.Wipe != "" {
		scope.Wipe = req.Wipe
	}
	scope = progress.NormalizeScope(scope)
	event := progress.TaskEvent{
		Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: id, Status: req.Status, Source: "manual", OccurredAt: time.Now().UTC(),
	}
	result := s.st.ApplyTaskEvents([]progress.TaskEvent{event})[0]
	writeJSON(w, map[string]any{"ok": result.Applied, "result": result})
}

func (s *Server) handlePutObjectiveProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Completed bool   `json:"completed"`
		Profile   string `json:"profile"`
		Mode      string `json:"mode"`
		Wipe      string `json:"wipe"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	scope := s.st.ActiveScope()
	if req.Profile != "" {
		scope.Profile = req.Profile
	}
	if req.Mode != "" {
		scope.Mode = req.Mode
	}
	if req.Wipe != "" {
		scope.Wipe = req.Wipe
	}
	scope = progress.NormalizeScope(scope)
	result := s.st.SetObjectiveStatus(scope, id, req.Completed)
	writeJSON(w, map[string]any{"ok": result.Applied, "result": result})
}

func (s *Server) handleIngestQuestEvents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []progress.TaskEvent `json:"events"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Events) > 100000 {
		writeError(w, http.StatusRequestEntityTooLarge, "too many events")
		return
	}
	results := s.st.ApplyTaskEvents(req.Events)
	applied, duplicate := 0, 0
	for _, result := range results {
		if result.Applied {
			applied++
		}
		if result.Duplicate {
			duplicate++
		}
	}
	writeJSON(w, map[string]any{"ok": true, "received": len(req.Events), "applied": applied, "duplicates": duplicate, "results": results})
}

func (s *Server) scanReplay(w http.ResponseWriter, r *http.Request, apply bool) {
	if s.logsDir == "" {
		writeError(w, http.StatusNotFound, "本机未配置游戏日志目录")
		return
	}
	scope := requestScope(r, s.st.ActiveScope())
	fromSession := r.URL.Query().Get("fromSession")
	sourceProfile := r.URL.Query().Get("sourceProfile")
	scan, err := questlog.ScanDirFrom(s.logsDir, scope, fromSession, sourceProfile)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if apply && fromSession == "" && len(scan.Breakpoints) > 1 {
		writeError(w, http.StatusBadRequest, "请选择与当前删档周期对应的日志起点")
		return
	}
	results := s.st.PreviewTaskEvents(scan.Events)
	if apply {
		results = s.st.ApplyTaskEvents(scan.Events)
	}
	applied, duplicates := 0, 0
	changes := make([]map[string]string, 0)
	for i, result := range results {
		if result.Applied {
			applied++
			changes = append(changes, map[string]string{"taskId": scan.Events[i].TaskID, "status": scan.Events[i].Status})
		}
		if result.Duplicate {
			duplicates++
		}
	}
	writeJSON(w, map[string]any{
		"ok": true, "applied": apply, "files": scan.Files, "sessions": scan.Sessions,
		"events": len(scan.Events), "changes": changes, "changeCount": applied,
		"duplicates": duplicates, "skipped": scan.Skipped, "modeSkipped": scan.ModeSkipped,
		"breakpoints": scan.Breakpoints,
	})
}

func (s *Server) handleLogReplayCandidates(w http.ResponseWriter, r *http.Request) {
	s.scanReplay(w, r, false)
}

func (s *Server) handleLogReplay(w http.ResponseWriter, r *http.Request) {
	s.scanReplay(w, r, true)
}

type ingestQuestReq struct {
	QuestID string `json:"questId"` // BSG gameId (log notification templateId prefix)
	Status  string `json:"status"`  // "completed", "failed", ...; empty clears
	EventID string `json:"eventId,omitempty"`
}

func (s *Server) handleIngestQuest(w http.ResponseWriter, r *http.Request) {
	var req ingestQuestReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.QuestID == "" {
		writeError(w, http.StatusBadRequest, "questId required")
		return
	}
	if !progress.ValidTaskStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "invalid task status")
		return
	}
	if s.qr.Get(req.QuestID) == nil {
		// Not fatal: quests without map markers still track status.
		log.Printf("quest %s not in located-quest registry (status %q)", req.QuestID, req.Status)
	}
	scope := s.st.ActiveScope()
	result := s.st.ApplyTaskEvents([]progress.TaskEvent{{
		EventID: req.EventID, Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
		TaskID: req.QuestID, Status: req.Status, Source: "live-log", OccurredAt: time.Now().UTC(),
	}})[0]
	log.Printf("quest: %s -> %q", req.QuestID, req.Status)
	writeJSON(w, map[string]any{"ok": result.Applied || result.Duplicate, "result": result})
}

func (s *Server) handleSetQuestStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Status string `json:"status"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.st.SetQuestStatus(id, req.Status)
	writeJSON(w, map[string]any{"ok": true})
}

// --- projection config ---

func (s *Server) handleSetProjection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p registry.Projection
	if err := decode(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.st.SetProjection(id, p) {
		writeError(w, http.StatusNotFound, "unknown map")
		return
	}
	log.Printf("projection updated for %s (calibrated=%v err=%.1fpx)", id, p.Calibrated, p.CalibrationError)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSetFloors(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var fr []registry.FloorRange
	if err := decode(r, &fr); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.st.SetFloorRanges(id, fr) {
		writeError(w, http.StatusNotFound, "unknown map")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- helpers ---

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}
