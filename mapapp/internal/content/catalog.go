package content

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
)

//go:embed data/default.json
var defaultPackJSON []byte

//go:embed data/media.json
var reviewedMediaJSON []byte

var upstreamMapIDs = map[string]string{
	"55f2d3fd4bdc2d5f408b4567": "factory",
	"59fc81d786f774390775787e": "factory",
	"56f40101d2720b2a4d8b45d6": "customs",
	"5704e3c2d2720bac5b8b4567": "woods",
	"5704e4dad2720bb55b8b4567": "lighthouse",
	"5704e554d2720bac5b8b456e": "shoreline",
	"5704e5fad2720bc05b8b4567": "reserve",
	"5714dbc024597771384a510d": "interchange",
	"5714dc692459777137212e12": "streetsoftarkov",
	"5b0fc42d86f7744a585f9105": "lab",
	"653e6760052c01c1c805532f": "groundzero",
	"65b8d6f5cdde2479cb2a3125": "groundzero",
	"68236e8153654e8c1200798a": "groundzero",
	"6a294a5b5eb5f9a1700417b7": "lab",
	"6733700029c367a3d40b02af": "labyrinth",
	"69af492a4819ea4ba10a69c5": "icebreaker",
	"65cc8f81a9aac3e77d0cfd3e": "terminal",
}

var supportedMapIDs = []string{
	"factory", "customs", "woods", "shoreline", "interchange", "lab",
	"reserve", "lighthouse", "streetsoftarkov", "groundzero", "terminal",
	"labyrinth", "icebreaker",
}

func InternalMapID(upstreamID string) (string, bool) {
	id, ok := upstreamMapIDs[upstreamID]
	return id, ok
}

type Catalog struct {
	pack   Pack
	source string
	byTask map[Mode]map[string]*Task
	media  map[string][]ObjectiveMedia
}

func Load(activePath string) (*Catalog, error) {
	b := defaultPackJSON
	source := "embedded"
	if activePath != "" {
		if external, err := os.ReadFile(activePath); err == nil {
			if _, err := decodeAndValidate(external); err == nil {
				b, source = external, activePath
			}
		}
	}
	pack, err := decodeAndValidate(b)
	if err != nil {
		return nil, fmt.Errorf("load content pack: %w", err)
	}
	var reviewed []ObjectiveMedia
	if err := json.Unmarshal(reviewedMediaJSON, &reviewed); err != nil {
		return nil, fmt.Errorf("load reviewed media: %w", err)
	}
	pack.Media = mergeMedia(pack.Media, reviewed)
	if err := validateMediaFiles(pack, MediaFS()); err != nil {
		return nil, fmt.Errorf("validate reviewed media: %w", err)
	}
	return NewCatalog(*pack, source), nil
}

func NewCatalog(pack Pack, source string) *Catalog {
	c := &Catalog{
		pack:   pack,
		source: source,
		byTask: map[Mode]map[string]*Task{},
		media:  map[string][]ObjectiveMedia{},
	}
	for _, item := range pack.Media {
		c.media[item.ObjectiveID] = append(c.media[item.ObjectiveID], item)
	}
	for mode, mc := range c.pack.Modes {
		c.byTask[mode] = map[string]*Task{}
		for i := range mc.Tasks {
			task := &mc.Tasks[i]
			for oi := range task.Objectives {
				obj := &task.Objectives[oi]
				if obj.ProgressID == "" {
					obj.ProgressID = task.ID + ":" + obj.ID
				}
				for _, media := range c.media[obj.ProgressID] {
					if media.MapID == "" || contains(obj.Maps, media.MapID) {
						obj.Media = append(obj.Media, media)
					}
				}
			}
			c.byTask[mode][task.ID] = task
		}
	}
	return c
}

func (c *Catalog) Meta() Meta {
	m := Meta{
		SchemaVersion: c.pack.SchemaVersion,
		Version:       c.pack.Version,
		GeneratedAt:   c.pack.GeneratedAt,
		Source:        c.source,
		Coverage:      map[Mode]map[string]MapCoverage{},
	}
	for mode, mc := range c.pack.Modes {
		m.Modes = append(m.Modes, mode)
		m.Coverage[mode] = mc.Coverage
	}
	sort.Slice(m.Modes, func(i, j int) bool { return m.Modes[i] < m.Modes[j] })
	return m
}

func (c *Catalog) Task(mode Mode, id string) *Task {
	return c.byTask[NormalizeMode(string(mode))][id]
}

func (c *Catalog) Tasks(mode Mode, mapID string) []Task {
	mc := c.pack.Modes[NormalizeMode(string(mode))]
	if mc == nil {
		return nil
	}
	out := make([]Task, 0)
	for _, task := range mc.Tasks {
		if task.Retired {
			continue
		}
		active := activeTaskCopy(task)
		if mapID == "" || taskOnMap(active, mapID) {
			out = append(out, active)
		}
	}
	return out
}

func activeTaskCopy(task Task) Task {
	active := task
	active.Objectives = make([]TaskObjective, 0, len(task.Objectives))
	for _, objective := range task.Objectives {
		if objective.Retired {
			continue
		}
		item := objective
		item.Locations = make([]ObjectiveLocation, 0, len(objective.Locations))
		for _, location := range objective.Locations {
			if !location.Retired {
				item.Locations = append(item.Locations, location)
			}
		}
		active.Objectives = append(active.Objectives, item)
	}
	return active
}

func (c *Catalog) Features(mode Mode, mapID string) []MapFeature {
	mc := c.pack.Modes[NormalizeMode(string(mode))]
	if mc == nil {
		return nil
	}
	out := make([]MapFeature, 0)
	for _, feature := range mc.Features {
		if mapID == "" || feature.MapID == mapID {
			out = append(out, feature)
		}
	}
	return out
}

func (c *Catalog) Coverage(mode Mode, mapID string) MapCoverage {
	if mc := c.pack.Modes[NormalizeMode(string(mode))]; mc != nil {
		return mc.Coverage[mapID]
	}
	return MapCoverage{MapID: mapID, Status: "missing", MessageZh: "公开数据暂缺"}
}

func (c *Catalog) Pack() Pack { return c.pack }

func taskOnMap(task Task, mapID string) bool {
	if task.MapID == mapID {
		return true
	}
	for _, obj := range task.Objectives {
		if contains(obj.Maps, mapID) {
			return true
		}
	}
	return false
}

func decodeAndValidate(b []byte) (*Pack, error) {
	var pack Pack
	if err := json.Unmarshal(b, &pack); err != nil {
		return nil, err
	}
	if err := normalizePack(&pack); err != nil {
		return nil, err
	}
	if err := Validate(&pack); err != nil {
		return nil, err
	}
	return &pack, nil
}

func normalizePack(pack *Pack) error {
	if err := applyStorylineOverlay(pack); err != nil {
		return err
	}
	for _, modeCatalog := range pack.Modes {
		if modeCatalog == nil {
			continue
		}
		for ti := range modeCatalog.Tasks {
			task := &modeCatalog.Tasks[ti]
			for oi := range task.Objectives {
				if task.Objectives[oi].ProgressID == "" {
					task.Objectives[oi].ProgressID = task.ID + ":" + task.Objectives[oi].ID
				}
			}
		}
	}
	return nil
}

func Validate(pack *Pack) error {
	if pack == nil {
		return errors.New("nil pack")
	}
	if pack.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version %d", pack.SchemaVersion)
	}
	if strings.TrimSpace(pack.Version) == "" {
		return errors.New("version required")
	}
	for _, mode := range []Mode{ModePVP, ModePVE} {
		mc := pack.Modes[mode]
		if mc == nil {
			return fmt.Errorf("mode %s missing", mode)
		}
		seenTask := map[string]bool{}
		seenObjective := map[string]bool{}
		seenFeature := map[string]bool{}
		seenStorylineOrder := map[int]bool{}
		for _, task := range mc.Tasks {
			if task.ID == "" || seenTask[task.ID] {
				return fmt.Errorf("mode %s has empty or duplicate task %q", mode, task.ID)
			}
			seenTask[task.ID] = true
			if task.Category == "storyline" {
				if task.StorylineOrder <= 0 || task.NameZh == "" {
					return fmt.Errorf("mode %s storyline task %s has incomplete metadata", mode, task.ID)
				}
				if seenStorylineOrder[task.StorylineOrder] {
					return fmt.Errorf("mode %s has duplicate storyline order %d", mode, task.StorylineOrder)
				}
				seenStorylineOrder[task.StorylineOrder] = true
			}
			for _, obj := range task.Objectives {
				if obj.ID == "" || obj.ProgressID == "" || seenObjective[obj.ProgressID] {
					return fmt.Errorf("mode %s task %s has empty or duplicate objective %q", mode, task.ID, obj.ProgressID)
				}
				seenObjective[obj.ProgressID] = true
				for _, mapID := range obj.Maps {
					if _, ok := supportedMapSet()[mapID]; !ok {
						return fmt.Errorf("task %s objective %s uses unsupported map %s", task.ID, obj.ID, mapID)
					}
				}
				for _, loc := range obj.Locations {
					if _, ok := supportedMapSet()[loc.MapID]; !ok {
						return fmt.Errorf("task %s objective %s uses unsupported location map %s", task.ID, obj.ID, loc.MapID)
					}
					if !finite(loc.Position) {
						return fmt.Errorf("task %s objective %s has invalid position", task.ID, obj.ID)
					}
				}
			}
		}
		for _, task := range mc.Tasks {
			for _, requirement := range task.Requirements {
				if !seenTask[requirement.TaskID] {
					return fmt.Errorf("task %s references missing prerequisite %s", task.ID, requirement.TaskID)
				}
			}
		}
		for _, feature := range mc.Features {
			key := feature.MapID + ":" + feature.Kind + ":" + feature.ID
			if feature.ID == "" || seenFeature[key] {
				return fmt.Errorf("mode %s has empty or duplicate feature %q", mode, key)
			}
			if _, ok := supportedMapSet()[feature.MapID]; !ok {
				return fmt.Errorf("feature %s uses unsupported map %s", feature.ID, feature.MapID)
			}
			if !finite(feature.Position) {
				return fmt.Errorf("feature %s has invalid position", feature.ID)
			}
			seenFeature[key] = true
		}
	}
	for _, media := range pack.Media {
		if media.ObjectiveID == "" || media.LocalFile == "" || media.SourceURL == "" ||
			media.Author == "" || media.License == "" || media.Checksum == "" || media.ReviewedAt.IsZero() {
			return fmt.Errorf("media for objective %q is not fully reviewed", media.ObjectiveID)
		}
		if strings.Contains(media.LocalFile, "..") || strings.HasPrefix(media.LocalFile, "/") {
			return fmt.Errorf("media %s has unsafe local path", media.LocalFile)
		}
	}
	return nil
}

func finite(v Vec3) bool {
	return !(math.IsNaN(v.X) || math.IsInf(v.X, 0) || math.IsNaN(v.Y) ||
		math.IsInf(v.Y, 0) || math.IsNaN(v.Z) || math.IsInf(v.Z, 0))
}

func supportedMapSet() map[string]struct{} {
	out := make(map[string]struct{}, len(supportedMapIDs))
	for _, id := range supportedMapIDs {
		out[id] = struct{}{}
	}
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func mergeMedia(a, b []ObjectiveMedia) []ObjectiveMedia {
	seen := map[string]bool{}
	out := make([]ObjectiveMedia, 0, len(a)+len(b))
	for _, list := range [][]ObjectiveMedia{a, b} {
		for _, item := range list {
			key := item.ObjectiveID + "\x00" + item.MapID + "\x00" + item.LocalFile
			if !seen[key] {
				seen[key] = true
				out = append(out, item)
			}
		}
	}
	return out
}

func validateMediaFiles(pack *Pack, mediaFS fs.FS) error {
	objectives := map[string]TaskObjective{}
	for _, modeCatalog := range pack.Modes {
		for _, task := range modeCatalog.Tasks {
			for _, objective := range task.Objectives {
				objectives[objective.ProgressID] = objective
			}
		}
	}
	for _, item := range pack.Media {
		objective, ok := objectives[item.ObjectiveID]
		if !ok {
			return fmt.Errorf("media %s references unknown objective %s", item.LocalFile, item.ObjectiveID)
		}
		if item.MapID != "" && !contains(objective.Maps, item.MapID) {
			return fmt.Errorf("media %s uses map %s outside objective %s", item.LocalFile, item.MapID, item.ObjectiveID)
		}
		data, err := fs.ReadFile(mediaFS, item.LocalFile)
		if err != nil {
			return fmt.Errorf("media %s: %w", item.LocalFile, err)
		}
		sum := sha256.Sum256(data)
		want := strings.TrimPrefix(strings.ToLower(item.Checksum), "sha256:")
		if hex.EncodeToString(sum[:]) != want {
			return fmt.Errorf("media %s checksum mismatch", item.LocalFile)
		}
		width, height, err := imageDimensions(data)
		if err != nil || width <= 0 || height <= 0 {
			return fmt.Errorf("media %s has invalid dimensions", item.LocalFile)
		}
	}
	return nil
}

func imageDimensions(data []byte) (int, int, error) {
	if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		return config.Width, config.Height, nil
	}
	if len(data) < 30 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, errors.New("unsupported image")
	}
	switch string(data[12:16]) {
	case "VP8X":
		width := 1 + int(data[24]) + int(data[25])<<8 + int(data[26])<<16
		height := 1 + int(data[27]) + int(data[28])<<8 + int(data[29])<<16
		return width, height, nil
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2f {
			break
		}
		width := 1 + int(data[21]) + int(data[22]&0x3f)<<8
		height := 1 + int(data[22]>>6) + int(data[23])<<2 + int(data[24]&0x0f)<<10
		return width, height, nil
	case "VP8 ":
		if index := bytes.Index(data[20:], []byte{0x9d, 0x01, 0x2a}); index >= 0 {
			index += 23
			if index+4 <= len(data) {
				width := int(data[index]) | int(data[index+1]&0x3f)<<8
				height := int(data[index+2]) | int(data[index+3]&0x3f)<<8
				return width, height, nil
			}
		}
	}
	return 0, 0, errors.New("invalid webp")
}
