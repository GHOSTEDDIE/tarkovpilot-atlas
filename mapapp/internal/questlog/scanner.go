// Package questlog parses task lifecycle notifications written by Escape from
// Tarkov. It reads log files only and does not inspect the game process.
package questlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"tarkovmap/internal/progress"
)

const Marker = "push-notifications|Got notification | ChatMessageReceived"

type notification struct {
	Message struct {
		Type       any    `json:"type"`
		TemplateID string `json:"templateId"`
	} `json:"message"`
}

type ScanResult struct {
	Events      []progress.TaskEvent `json:"events"`
	Files       int                  `json:"files"`
	Sessions    int                  `json:"sessions"`
	Skipped     int                  `json:"skipped"`
	ModeSkipped int                  `json:"modeSkipped"`
	Breakpoints []Breakpoint         `json:"breakpoints,omitempty"`
}

type Breakpoint struct {
	SessionID     string    `json:"sessionId"`
	StartedAt     time.Time `json:"startedAt,omitempty"`
	GameVersion   string    `json:"gameVersion,omitempty"`
	GameProfileID string    `json:"gameProfileId,omitempty"`
	AccountID     string    `json:"accountId,omitempty"`
	Mode          string    `json:"mode,omitempty"`
}

var (
	sessionModeRe = regexp.MustCompile(`(?i)Session mode:\s*(Pve|PVE|Regular|PVP)`)
	profileRe     = regexp.MustCompile(`(SelectProfile|SelectedProfile|PrepareSelectedProfileLocally) ProfileId:(\w+) AccountId:(\d+)`)
	versionRe     = regexp.MustCompile(`Init:\s*pstrGameVersion:\s*([^\s|]+)`)
)

func StatusFromNotification(templateID string, rawType any) (taskID, status string, ok bool) {
	parts := strings.Fields(templateID)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	taskID = parts[0]
	switch statusString(rawType) {
	case "10":
		return taskID, progress.StatusStarted, true
	case "11":
		return taskID, progress.StatusFailed, true
	case "12":
		return taskID, progress.StatusCompleted, true
	}
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.ToLower(strings.Join(parts[1:], " "))
	}
	switch {
	case strings.Contains(suffix, "successmessagetext"):
		status = progress.StatusCompleted
	case strings.Contains(suffix, "failmessagetext"):
		status = progress.StatusFailed
	case strings.Contains(suffix, "startmessagetext"), strings.Contains(suffix, "acceptmessagetext"):
		status = progress.StatusStarted
	default:
		return "", "", false
	}
	return taskID, status, true
}

func ScanDir(logsDir string, scope progress.Scope) (ScanResult, error) {
	return ScanDirFrom(logsDir, scope, "", "")
}

func ScanDirFrom(logsDir string, scope progress.Scope, fromSession, sourceProfile string) (ScanResult, error) {
	result := ScanResult{}
	scope = progress.NormalizeScope(scope)
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return result, err
	}
	var sessions []string
	for _, entry := range entries {
		if entry.IsDir() {
			sessions = append(sessions, filepath.Join(logsDir, entry.Name()))
		}
	}
	sort.Strings(sessions)
	result.Sessions = len(sessions)
	lastBreakpointKey := ""
	for _, session := range sessions {
		meta := inspectSession(session)
		breakpointKey := meta.GameProfileID + "\x00" + meta.Mode + "\x00" + meta.GameVersion
		if len(result.Breakpoints) == 0 || (breakpointKey != "\x00\x00" && breakpointKey != lastBreakpointKey) {
			result.Breakpoints = append(result.Breakpoints, meta)
			lastBreakpointKey = breakpointKey
		}
		if fromSession != "" && filepath.Base(session) < fromSession {
			continue
		}
		if sourceProfile != "" && meta.GameProfileID != sourceProfile {
			continue
		}
		if meta.Mode != "" && meta.Mode != scope.Mode {
			result.ModeSkipped++
			continue
		}
		paths, _ := filepath.Glob(filepath.Join(session, "*push-notifications_*.log"))
		sort.Strings(paths)
		for _, path := range paths {
			events, skipped, err := ScanFile(path, scope)
			if err != nil {
				return result, err
			}
			result.Events = append(result.Events, events...)
			result.Files++
			result.Skipped += skipped
		}
	}
	sort.SliceStable(result.Events, func(i, j int) bool {
		if result.Events[i].OccurredAt.Equal(result.Events[j].OccurredAt) {
			return result.Events[i].EventID < result.Events[j].EventID
		}
		return result.Events[i].OccurredAt.Before(result.Events[j].OccurredAt)
	})
	return result, nil
}

func inspectSession(session string) Breakpoint {
	meta := Breakpoint{SessionID: filepath.Base(session)}
	if info, err := os.Stat(session); err == nil {
		meta.StartedAt = info.ModTime().UTC()
	}
	paths, _ := filepath.Glob(filepath.Join(session, "*application_*.log"))
	sort.Strings(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, match := range sessionModeRe.FindAllSubmatch(data, -1) {
			switch strings.ToLower(string(match[1])) {
			case "pve":
				meta.Mode = "pve"
			default:
				meta.Mode = "pvp"
			}
		}
		for _, match := range profileRe.FindAllSubmatch(data, -1) {
			meta.GameProfileID, meta.AccountID = string(match[2]), string(match[3])
		}
		for _, match := range versionRe.FindAllSubmatch(data, -1) {
			meta.GameVersion = string(match[1])
		}
	}
	return meta
}

func ScanFile(path string, scope progress.Scope) ([]progress.TaskEvent, int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	scope = progress.NormalizeScope(scope)
	lines := splitLines(b)
	var events []progress.TaskEvent
	skipped := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !bytes.Contains(line.text, []byte(Marker)) {
			continue
		}
		markerOffset := line.offset
		occurredAt := parseLogTime(line.text)
		var payload []byte
		if brace := bytes.IndexByte(line.text, '{'); brace >= 0 {
			payload = append(payload, line.text[brace:]...)
		}
		for i++; i < len(lines); i++ {
			if looksDated(lines[i].text) {
				i--
				break
			}
			payload = append(payload, lines[i].text...)
			payload = append(payload, '\n')
		}
		var record notification
		if len(bytes.TrimSpace(payload)) == 0 || json.Unmarshal(payload, &record) != nil {
			skipped++
			continue
		}
		taskID, status, ok := StatusFromNotification(record.Message.TemplateID, record.Message.Type)
		if !ok {
			skipped++
			continue
		}
		if occurredAt.IsZero() {
			occurredAt = time.Unix(0, markerOffset).UTC()
		}
		fileID := filepath.Clean(path)
		eventID := progress.EventKey(fileID, markerOffset, taskID, status)
		events = append(events, progress.TaskEvent{
			EventID: eventID, Profile: scope.Profile, Mode: scope.Mode, Wipe: scope.Wipe,
			TaskID: taskID, Status: status, Source: "history-log", OccurredAt: occurredAt,
			FileID: fileID, Offset: markerOffset,
		})
	}
	return events, skipped, nil
}

type line struct {
	offset int64
	text   []byte
}

func splitLines(data []byte) []line {
	var out []line
	scanner := bufio.NewScanner(bytes.NewReader(data))
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 4*1024*1024)
	var offset int64
	for scanner.Scan() {
		text := append([]byte(nil), scanner.Bytes()...)
		out = append(out, line{offset: offset, text: text})
		offset += int64(len(text) + 1)
	}
	return out
}

func looksDated(value []byte) bool {
	if len(value) < len("2006-01-02 0:00:00.000") {
		return false
	}
	_, err := time.Parse("2006-01-02 15:04:05.000", firstTimestamp(value))
	return err == nil
}

func parseLogTime(value []byte) time.Time {
	parsed, _ := time.ParseInLocation("2006-01-02 15:04:05.000", firstTimestamp(value), time.Local)
	return parsed.UTC()
}

func firstTimestamp(value []byte) string {
	fields := strings.Fields(string(value))
	if len(fields) < 2 {
		return ""
	}
	return fields[0] + " " + fields[1]
}

func statusString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}
