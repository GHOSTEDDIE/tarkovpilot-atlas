// HTTP server: REST ingest API + SSE event stream + embedded web UI.
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"tarkovmap/internal/gamelogs"
	"tarkovmap/internal/parser"
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
	st       *store.Store
	reg      *registry.Registry
	qr       *quests.Registry
	token    string // optional ingest token; empty = open
	svgBase  string
	mux      *http.ServeMux

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

type Config struct {
	Token      string // if set, ingest POSTs need X-Token or ?token=
	SVGBaseURL string // where the web UI fetches map SVGs from
}

func New(st *store.Store, reg *registry.Registry, qr *quests.Registry, cfg Config) *Server {
	s := &Server{
		st:       st,
		reg:      reg,
		qr:       qr,
		token:    cfg.Token,
		svgBase:  cfg.SVGBaseURL,
		clients:  map[chan []byte]struct{}{},
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
	mux.Handle("GET /maps/", http.StripPrefix("/maps/", http.FileServer(http.FS(mapsSub))))

	s.mux = mux
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

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
	MapID       string                   `json:"mapId"`
	Maps        map[string]*registry.Map `json:"maps"`
	Position    *store.PositionEvent     `json:"position"`
	History     []store.PositionEvent    `json:"history"`
	QuestStatus map[string]string        `json:"questStatus"`
	Settings    store.Settings           `json:"settings"`
	SVGBaseURL  string                   `json:"svgBaseUrl"`
	ServerTime  time.Time                `json:"serverTime"`
}

func (s *Server) snapshot() stateResponse {
	return stateResponse{
		MapID:       s.st.CurrentMap(),
		Maps:        s.st.Maps(),
		Position:    s.st.Position(),
		History:     s.st.History(),
		QuestStatus: s.st.QuestStatuses(),
		Settings:    s.st.GetSettings(),
		SVGBaseURL:  s.svgBase,
		ServerTime:  time.Now().UTC(),
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

type ingestQuestReq struct {
	QuestID string `json:"questId"` // BSG gameId (log notification templateId prefix)
	Status  string `json:"status"`  // "completed", "failed", ...; empty clears
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
	if s.qr.Get(req.QuestID) == nil {
		// Not fatal: quests without map markers still track status.
		log.Printf("quest %s not in located-quest registry (status %q)", req.QuestID, req.Status)
	}
	s.st.SetQuestStatus(req.QuestID, req.Status)
	log.Printf("quest: %s -> %q", req.QuestID, req.Status)
	writeJSON(w, map[string]any{"ok": true})
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
