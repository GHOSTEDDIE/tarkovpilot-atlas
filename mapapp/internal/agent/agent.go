// Agent mode: runs on the gaming PC, watches the EFT screenshots folder
// and game logs, and pushes parsed events to the map server over HTTP.
//
// Read-only by design (PLAN §7): it never touches the game process,
// game memory, or game files — it only reads screenshot FILE NAMES and
// log lines the game itself writes.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

var (
	// PVP: "TRACE-NetworkGameCreate profileStatus ... location: bigmap, ..."
	locationRe = regexp.MustCompile(`(?i)location:\s*(\S+),`)
	// PVE: "scene preset ... path:maps/factory4_day.bundle"
	locationRe2 = regexp.MustCompile(`(?i)path:maps/(\w+)\.bundle`)

	// a log line starting with a date — ends a notification JSON block
	lineStartWithDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{1,2}:\d{1,2}:\d{1,2}\.\d{3}`)
)

// quest notification marker (push-notifications_*.log)
const taskSubstring = "push-notifications|Got notification | ChatMessageReceived"

// pushNotification — the part of the notification JSON block we care about
type pushNotification struct {
	Message struct {
		Type       any    `json:"type"`
		TemplateID string `json:"templateId"`
	} `json:"message"`
}

// questStatusFromTemplate derives a quest status from the notification
// templateId, e.g. "6574e0de… successMessageText" → ("6574e0de…", "completed").
func questStatusFromTemplate(templateID string, rawType string) (questID, status string, ok bool) {
	parts := strings.Fields(templateID)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	questID = parts[0]
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.ToLower(parts[1])
	}
	switch {
	case strings.Contains(suffix, "successmessagetext"):
		status = "completed"
	case strings.Contains(suffix, "failmessagetext"):
		status = "failed"
	case strings.Contains(suffix, "startmessagetext"), strings.Contains(suffix, "acceptmessagetext"):
		status = "started"
	default:
		status = rawType // unknown template — keep the raw type for debugging
	}
	return questID, status, true
}

type Config struct {
	ServerURL      string // e.g. http://192.168.1.10:8400
	Token          string
	ScreenshotsDir string
	LogsDir        string
}

type Agent struct {
	cfg    Config
	client *http.Client

	seenScreens map[string]bool
	lastMapSent string

	logSession  string
	logPosition map[string]int64

	startedAt        time.Time
	cleanScreenshots bool // polled from the server (web UI toggle)
}

func New(cfg Config) *Agent {
	return &Agent{
		cfg:         cfg,
		client:      &http.Client{Timeout: 5 * time.Second},
		seenScreens: map[string]bool{},
		logPosition: map[string]int64{},
		startedAt:   time.Now(),
	}
}

func (a *Agent) Run(ctx context.Context) {
	if a.cfg.ScreenshotsDir != "" {
		a.watchScreenshots(ctx)
	}
	if a.cfg.LogsDir == "" {
		log.Printf("agent: screenshots=%q server=%s (no logs dir — map auto-detect off)", a.cfg.ScreenshotsDir, a.cfg.ServerURL)
		<-ctx.Done()
		return
	}
	log.Printf("agent: screenshots=%q logs=%q server=%s", a.cfg.ScreenshotsDir, a.cfg.LogsDir, a.cfg.ServerURL)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	lastConfigPoll := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if time.Since(lastConfigPoll) > 15*time.Second {
				lastConfigPoll = time.Now()
				a.pollConfig()
			}
			a.scanLogs()
		}
	}
}

// pollConfig fetches server settings (e.g. the web UI's clean-screenshots toggle).
func (a *Agent) pollConfig() {
	url := strings.TrimSuffix(a.cfg.ServerURL, "/") + "/api/config"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	if a.cfg.Token != "" {
		req.Header.Set("X-Token", a.cfg.Token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var cfg struct {
		CleanScreenshots bool `json:"cleanScreenshots"`
	}
	if json.NewDecoder(resp.Body).Decode(&cfg) == nil && cfg.CleanScreenshots != a.cleanScreenshots {
		a.cleanScreenshots = cfg.CleanScreenshots
		log.Printf("agent: cleanScreenshots = %v", a.cleanScreenshots)
	}
}

// cleanSessionScreenshots deletes *.png files created after the agent
// started (TarkovPilot parity). Never touches older files.
func (a *Agent) cleanSessionScreenshots() int {
	if a.cfg.ScreenshotsDir == "" {
		return 0
	}
	entries, err := os.ReadDir(a.cfg.ScreenshotsDir)
	if err != nil {
		return 0
	}
	deleted := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().Before(a.startedAt) {
			continue
		}
		if os.Remove(filepath.Join(a.cfg.ScreenshotsDir, e.Name())) == nil {
			deleted++
		}
	}
	return deleted
}

// --- screenshots ---

// watchScreenshots reacts to new *.png files via fsnotify. The initial
// directory scan only marks existing files as seen — never uploads them.
func (a *Agent) watchScreenshots(ctx context.Context) {
	if fi, err := os.Stat(a.cfg.ScreenshotsDir); err != nil || !fi.IsDir() {
		log.Printf("agent: screenshots dir %q not found — screenshot tracking off", a.cfg.ScreenshotsDir)
		return
	}
	entries, _ := os.ReadDir(a.cfg.ScreenshotsDir)
	for _, e := range entries {
		a.seenScreens[e.Name()] = true
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("agent: fsnotify unavailable (%v) — screenshot tracking off", err)
		return
	}
	if err := fsw.Add(a.cfg.ScreenshotsDir); err != nil {
		fsw.Close()
		log.Printf("agent: watch screenshots: %v", err)
		return
	}
	go func() {
		defer fsw.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-fsw.Events:
				if !ok {
					return
				}
				if !ev.Op.Has(fsnotify.Create) || !strings.EqualFold(filepath.Ext(ev.Name), ".png") {
					continue
				}
				name := filepath.Base(ev.Name)
				if a.seenScreens[name] {
					continue
				}
				a.seenScreens[name] = true
				// Only file names are sent — never the image content (PLAN §5).
				if err := a.post("/api/ingest/screenshot", map[string]string{"filename": name}); err != nil {
					log.Printf("agent: send screenshot: %v", err)
					delete(a.seenScreens, name) // retry if the file event repeats
				} else {
					log.Printf("agent: screenshot %s", name)
				}
			case err, ok := <-fsw.Errors:
				if !ok {
					return
				}
				log.Printf("agent: screenshot watcher: %v", err)
			}
		}
	}()
}

// --- logs ---

func (a *Agent) scanLogs() {
	session := newestDir(a.cfg.LogsDir)
	if session == "" {
		return
	}
	if session != a.logSession {
		a.logSession = session
		a.logPosition = map[string]int64{}
		log.Printf("agent: log session %s", filepath.Base(session))
	}

	var files []string
	for _, pattern := range []string{"*application_*.log", "*push-notifications_*.log"} {
		matches, _ := filepath.Glob(filepath.Join(session, pattern))
		files = append(files, matches...)
	}
	for _, path := range files {
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		pos, known := a.logPosition[path]
		if !known {
			// First time we see the file: the app may have started after the
			// game entered a map (PLAN 阶段 2), so replay only the LAST
			// location line from the existing content, then tail.
			a.logPosition[path] = fi.Size()
			lines, _ := readNewLines(path, 0)
			last := ""
			for _, line := range lines {
				if raw := matchLocation(line); raw != "" {
					last = raw
				}
			}
			if last != "" {
				a.sendMap(last)
			}
			continue
		}
		lines, newPos := readNewLines(path, pos)
		a.logPosition[path] = newPos
		a.processLines(lines)
	}
}

// processLines handles new log lines: map location lines are single-line,
// quest notifications are a marker line followed by a multi-line JSON block
// (same parsing as TarkovPilot's LogsWatcher).
func (a *Agent) processLines(lines []string) {
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}

		if raw := matchLocation(line); raw != "" {
			a.sendMap(raw)
			continue
		}

		if !strings.Contains(line, taskSubstring) {
			continue
		}

		// the notification JSON spans the following lines until a dated line
		var sb strings.Builder
		i++
		for i < len(lines) {
			jl := lines[i]
			if lineStartWithDateRe.MatchString(jl) {
				i-- // the dated line belongs to the outer loop
				break
			}
			sb.WriteString(jl)
			sb.WriteString("\n")
			i++
		}

		jsonStr := strings.TrimSpace(sb.String())
		if jsonStr == "" {
			continue
		}
		var rec pushNotification
		if err := json.Unmarshal([]byte(jsonStr), &rec); err != nil {
			continue // incomplete/broken block — skip
		}
		if rec.Message.TemplateID == "" {
			continue
		}
		questID, status, ok := questStatusFromTemplate(rec.Message.TemplateID, statusToString(rec.Message.Type))
		if !ok {
			continue
		}
		a.sendQuest(questID, status)
	}
}

func (a *Agent) sendQuest(questID, status string) {
	if err := a.post("/api/ingest/quest", map[string]string{"questId": questID, "status": status}); err != nil {
		log.Printf("agent: send quest: %v", err)
		return
	}
	log.Printf("agent: quest %s -> %s", questID, status)
}

// statusToString — message.type in the log can be a number or a string
func statusToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// matchLocation extracts the raw map preset from a log line, PVP or PVE.
func matchLocation(line string) string {
	if strings.Contains(line, "application|TRACE-NetworkGameCreate profileStatus") {
		if m := locationRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	} else if strings.Contains(line, "application|scene preset") {
		if m := locationRe2.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

func (a *Agent) sendMap(raw string) {
	if strings.EqualFold(raw, a.lastMapSent) {
		return
	}
	if err := a.post("/api/ingest/map", map[string]string{"map": raw}); err != nil {
		log.Printf("agent: send map: %v", err)
		return
	}
	// a real map change (not the first detection after startup) triggers the
	// optional session-screenshot cleanup
	if a.lastMapSent != "" && a.cleanScreenshots {
		if n := a.cleanSessionScreenshots(); n > 0 {
			log.Printf("agent: cleaned %d session screenshots", n)
		}
	}
	a.lastMapSent = raw
}

func newestDir(parent string) string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	best := ""
	var bestTime time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || fi.ModTime().After(bestTime) {
			best, bestTime = filepath.Join(parent, e.Name()), fi.ModTime()
		}
	}
	return best
}

// readNewLines reads new complete lines starting at pos; an unfinished
// tail waits for the next tick (same approach as TarkovPilot's watcher).
func readNewLines(path string, pos int64) ([]string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, pos
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, pos
	}
	if pos > fi.Size() {
		pos = 0 // rotated
	}
	if _, err := f.Seek(pos, io.SeekStart); err != nil {
		return nil, pos
	}
	b, err := io.ReadAll(f)
	if err != nil || len(b) == 0 {
		return nil, pos
	}
	content := string(b)
	lastNl := strings.LastIndexByte(content, '\n')
	if lastNl < 0 {
		return nil, pos
	}
	complete := content[:lastNl+1]
	lines := strings.Split(strings.ReplaceAll(complete, "\r\n", "\n"), "\n")
	return lines[:len(lines)-1], pos + int64(len(complete))
}

// --- http ---

func (a *Agent) post(path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := strings.TrimSuffix(a.cfg.ServerURL, "/") + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.Token != "" {
		req.Header.Set("X-Token", a.cfg.Token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("server %s: %s", resp.Status, strings.TrimSpace(string(rb)))
	}
	return nil
}
