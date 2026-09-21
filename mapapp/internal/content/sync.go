package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const DefaultUpstreamBaseURL = "https://json.tarkov.dev"

type RefreshOptions struct {
	BaseURL string
	Target  string
	Apply   bool
	Client  *http.Client
	Now     func() time.Time
}

type ModeCounts struct {
	Tasks      int `json:"tasks"`
	Objectives int `json:"objectives"`
	Locations  int `json:"locations"`
	Extracts   int `json:"extracts"`
	Transits   int `json:"transits"`
}

type RefreshReport struct {
	Version     string              `json:"version"`
	GeneratedAt time.Time           `json:"generatedAt"`
	Applied     bool                `json:"applied"`
	Unchanged   bool                `json:"unchanged"`
	Target      string              `json:"target,omitempty"`
	Modes       map[Mode]ModeCounts `json:"modes"`
	Previous    map[Mode]ModeCounts `json:"previous,omitempty"`
	Changes     map[Mode]ModeCounts `json:"changes,omitempty"`
	Preserved   map[Mode]ModeCounts `json:"preserved,omitempty"`
	Sources     []SourceInfo        `json:"sources"`
	Warnings    []string            `json:"warnings,omitempty"`
	Hash        string              `json:"sha256"`
	Pack        *Pack               `json:"-"`
}

type rawTasksEnvelope struct {
	Data struct {
		Tasks map[string]rawTask `json:"tasks"`
	} `json:"data"`
}

type rawTask struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	Trader              string           `json:"trader"`
	Map                 string           `json:"map"`
	WikiLink            string           `json:"wikiLink"`
	MinPlayerLevel      int              `json:"minPlayerLevel"`
	FactionName         string           `json:"factionName"`
	KappaRequired       bool             `json:"kappaRequired"`
	LightkeeperRequired bool             `json:"lightkeeperRequired"`
	TaskImageLink       string           `json:"taskImageLink"`
	TaskRequirements    []rawRequirement `json:"taskRequirements"`
	Objectives          []rawObjective   `json:"objectives"`
}

type rawRequirement struct {
	Task   string   `json:"task"`
	Status []string `json:"status"`
}

type rawObjective struct {
	ID                string                `json:"id"`
	Description       string                `json:"description"`
	Type              string                `json:"type"`
	Optional          bool                  `json:"optional"`
	Maps              []string              `json:"maps"`
	Zones             []rawZone             `json:"zones"`
	PossibleLocations []rawPossibleLocation `json:"possibleLocations"`
}

type rawZone struct {
	ID       string   `json:"id"`
	Map      string   `json:"map"`
	Position Vec3     `json:"position"`
	Outline  []Vec3   `json:"outline"`
	Top      *float64 `json:"top"`
	Bottom   *float64 `json:"bottom"`
}

type rawPossibleLocation struct {
	Map       string `json:"map"`
	Positions []Vec3 `json:"positions"`
}

type rawMapsEnvelope struct {
	Data struct {
		Maps map[string]rawMap `json:"maps"`
	} `json:"data"`
}

type rawMap struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	NormalizedName string       `json:"normalizedName"`
	ScenePath      string       `json:"scenePath"`
	Extracts       []rawExtract `json:"extracts"`
	Transits       []rawTransit `json:"transits"`
}

type rawExtract struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Faction      string            `json:"faction"`
	Position     Vec3              `json:"position"`
	Outline      []Vec3            `json:"outline"`
	Top          *float64          `json:"top"`
	Bottom       *float64          `json:"bottom"`
	Switches     []json.RawMessage `json:"switches"`
	TransferItem json.RawMessage   `json:"transferItem"`
}

type rawTransit struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Map         string   `json:"map"`
	Position    Vec3     `json:"position"`
	Outline     []Vec3   `json:"outline"`
	Top         *float64 `json:"top"`
	Bottom      *float64 `json:"bottom"`
}

type rawTradersEnvelope struct {
	Data map[string]rawTrader `json:"data"`
}

type rawTrader struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type rawTranslationEnvelope struct {
	Data map[string]string `json:"data"`
}

type upstreamMode struct {
	Tasks   map[string]rawTask
	Maps    map[string]rawMap
	Traders map[string]rawTrader
	English map[string]string
	Chinese map[string]string
	Sources []SourceInfo
	MapIDs  map[string]string
}

func Refresh(ctx context.Context, opts RefreshOptions) (*RefreshReport, error) {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = DefaultUpstreamBaseURL
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}

	pack := &Pack{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now,
		Modes:         map[Mode]*ModeCatalog{},
	}
	report := &RefreshReport{
		GeneratedAt: now,
		Target:      opts.Target,
		Modes:       map[Mode]ModeCounts{},
		Previous:    map[Mode]ModeCounts{},
		Changes:     map[Mode]ModeCounts{},
		Preserved:   map[Mode]ModeCounts{},
		Pack:        pack,
	}
	for _, pair := range []struct {
		mode Mode
		path string
	}{{ModePVP, "regular"}, {ModePVE, "pve"}} {
		up, err := fetchUpstreamMode(ctx, client, base, pair.path)
		if err != nil {
			return nil, fmt.Errorf("download %s content: %w", pair.mode, err)
		}
		mc, _, warnings := buildMode(up)
		pack.Modes[pair.mode] = mc
		report.Warnings = append(report.Warnings, warnings...)
		pack.Sources = append(pack.Sources, up.Sources...)
	}
	if err := applyStorylineOverlay(pack); err != nil {
		return nil, err
	}
	for _, mode := range []Mode{ModePVP, ModePVE} {
		report.Modes[mode] = catalogCounts(pack.Modes[mode])
	}

	var existingPack *Pack
	if existing, err := Load(opts.Target); err == nil {
		loadedPack := existing.Pack()
		existingPack = &loadedPack
		pack.Media = existingPack.Media
		for _, mode := range []Mode{ModePVP, ModePVE} {
			if catalogsCompatible(pack.Modes[mode], existingPack.Modes[mode]) {
				report.Preserved[mode] = preserveKnownGoodData(pack.Modes[mode], existingPack.Modes[mode])
				if preserved := report.Preserved[mode]; preserved.Tasks+preserved.Objectives+preserved.Locations > 0 {
					report.Warnings = append(report.Warnings, fmt.Sprintf(
						"%s 保留上一有效版本：退役任务 %d、退役目标 %d、已验证坐标 %d",
						mode, preserved.Tasks, preserved.Objectives, preserved.Locations,
					))
				}
			}
			report.Previous[mode] = catalogCounts(existingPack.Modes[mode])
			report.Modes[mode] = catalogCounts(pack.Modes[mode])
			report.Changes[mode] = subtractCounts(report.Modes[mode], report.Previous[mode])
		}
	}
	sort.Slice(pack.Sources, func(i, j int) bool { return pack.Sources[i].URL < pack.Sources[j].URL })
	report.Sources = append([]SourceInfo(nil), pack.Sources...)
	fingerprint, err := packFingerprint(pack)
	if err != nil {
		return nil, err
	}
	pack.Version = now.Format("20060102T150405Z") + "-" + fingerprint[:12]
	if err := Validate(pack); err != nil {
		return nil, fmt.Errorf("validate refreshed content: %w", err)
	}
	if err := validateMediaFiles(pack, MediaFS()); err != nil {
		return nil, fmt.Errorf("validate reviewed media: %w", err)
	}
	report.Version = pack.Version
	report.Hash = fingerprint
	if existingPack != nil {
		existingFingerprint, err := packFingerprint(existingPack)
		if err != nil {
			return nil, err
		}
		if existingFingerprint == fingerprint || strings.HasSuffix(existingPack.Version, "-"+fingerprint[:12]) {
			pack.Version = existingPack.Version
			pack.GeneratedAt = existingPack.GeneratedAt
			report.Version = existingPack.Version
			report.Unchanged = true
			return report, nil
		}
	}

	if opts.Apply {
		if opts.Target == "" {
			return nil, errors.New("target is required when applying a refresh")
		}
		if err := writePackAtomic(opts.Target, pack); err != nil {
			return nil, err
		}
		report.Applied = true
	}
	return report, nil
}

func Rollback(target string) error {
	if target == "" {
		return errors.New("target is required")
	}
	previous := target + ".previous"
	if _, err := decodePackFile(previous); err != nil {
		return fmt.Errorf("previous content pack: %w", err)
	}
	current := target + ".rollback"
	_ = os.Remove(current)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, current); err != nil {
			return fmt.Errorf("preserve current content pack: %w", err)
		}
	}
	if err := os.Rename(previous, target); err != nil {
		_ = os.Rename(current, target)
		return fmt.Errorf("activate previous content pack: %w", err)
	}
	_ = os.Rename(current, previous)
	return nil
}

func fetchUpstreamMode(ctx context.Context, client *http.Client, base, path string) (*upstreamMode, error) {
	up := &upstreamMode{English: map[string]string{}, Chinese: map[string]string{}}
	fetch := func(endpoint string, dst any) error {
		url := base + "/" + path + "/" + endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 128<<20)).Decode(dst); err != nil {
			return fmt.Errorf("decode %s: %w", url, err)
		}
		up.Sources = append(up.Sources, SourceInfo{
			URL:          url,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		})
		return nil
	}
	var tasks rawTasksEnvelope
	var maps rawMapsEnvelope
	var traders rawTradersEnvelope
	if err := fetch("tasks", &tasks); err != nil {
		return nil, err
	}
	if err := fetch("maps", &maps); err != nil {
		return nil, err
	}
	if err := fetch("traders", &traders); err != nil {
		return nil, err
	}
	for _, locale := range []struct {
		name string
		dst  *map[string]string
	}{{"tasks_en", &up.English}, {"tasks_zh", &up.Chinese}, {"maps_en", &up.English},
		{"maps_zh", &up.Chinese}, {"traders_en", &up.English}, {"traders_zh", &up.Chinese}} {
		var env rawTranslationEnvelope
		if err := fetch(locale.name, &env); err != nil {
			return nil, err
		}
		for key, value := range env.Data {
			(*locale.dst)[key] = value
		}
	}
	up.Tasks, up.Maps, up.Traders = tasks.Data.Tasks, maps.Data.Maps, traders.Data
	up.MapIDs = dynamicMapIDs(up.Maps)
	return up, nil
}

func dynamicMapIDs(maps map[string]rawMap) map[string]string {
	resolved := make(map[string]string, len(maps))
	for key, item := range maps {
		upstreamID := item.ID
		if upstreamID == "" {
			upstreamID = key
		}
		if mapID := canonicalMapID(item.NormalizedName); mapID != "" {
			resolved[upstreamID] = mapID
		}
	}
	return resolved
}

func canonicalMapID(normalizedName string) string {
	name := strings.ToLower(strings.TrimSpace(normalizedName))
	aliases := map[string]string{
		"factory":              "factory",
		"night-factory":        "factory",
		"customs":              "customs",
		"woods":                "woods",
		"shoreline":            "shoreline",
		"interchange":          "interchange",
		"the-lab":              "lab",
		"the-lab-dark":         "lab",
		"reserve":              "reserve",
		"lighthouse":           "lighthouse",
		"streets-of-tarkov":    "streetsoftarkov",
		"ground-zero":          "groundzero",
		"ground-zero-21":       "groundzero",
		"ground-zero-tutorial": "groundzero",
		"terminal":             "terminal",
		"the-labyrinth":        "labyrinth",
		"icebreaker":           "icebreaker",
	}
	mapID := aliases[name]
	if _, ok := supportedMapSet()[mapID]; !ok {
		return ""
	}
	return mapID
}

func resolveMapID(up *upstreamMode, upstreamID string) (string, bool) {
	if up != nil {
		if mapID, ok := up.MapIDs[upstreamID]; ok {
			return mapID, true
		}
	}
	return InternalMapID(upstreamID)
}

func buildMode(up *upstreamMode) (*ModeCatalog, ModeCounts, []string) {
	mc := &ModeCatalog{Coverage: map[string]MapCoverage{}}
	counts := ModeCounts{}
	warnings := []string{}
	missingChinese := 0
	omittedMapReferences := 0
	for _, id := range supportedMapIDs {
		mc.Coverage[id] = MapCoverage{MapID: id, Status: "missing", MessageZh: "公开撤离点数据暂缺"}
	}

	traders := map[string]rawTrader{}
	for key, trader := range up.Traders {
		if trader.ID == "" {
			trader.ID = key
		}
		traders[trader.ID] = trader
	}
	for key, raw := range up.Tasks {
		if raw.ID == "" {
			raw.ID = key
		}
		task := Task{
			ID:                  raw.ID,
			Name:                translated(up.English, raw.Name),
			NameZh:              translated(up.Chinese, raw.Name),
			TraderID:            raw.Trader,
			MinPlayerLevel:      raw.MinPlayerLevel,
			Faction:             normalizeFaction(raw.FactionName),
			KappaRequired:       raw.KappaRequired,
			LightkeeperRequired: raw.LightkeeperRequired,
			WikiLink:            raw.WikiLink,
			TaskImageLink:       raw.TaskImageLink,
		}
		if _, ok := up.Chinese[raw.Name]; !ok {
			missingChinese++
		}
		if mapID, ok := resolveMapID(up, raw.Map); ok {
			task.MapID = mapID
		} else if raw.Map != "" {
			omittedMapReferences++
		}
		if trader, ok := traders[raw.Trader]; ok {
			task.Trader = translated(up.English, trader.Name)
			task.TraderZh = translated(up.Chinese, trader.Name)
		}
		for _, requirement := range raw.TaskRequirements {
			task.Requirements = append(task.Requirements, Requirement{TaskID: requirement.Task, Statuses: requirement.Status})
		}
		for _, rawObj := range raw.Objectives {
			obj := TaskObjective{
				ID:            rawObj.ID,
				ProgressID:    raw.ID + ":" + rawObj.ID,
				Description:   translated(up.English, rawObj.Description),
				DescriptionZh: translated(up.Chinese, rawObj.Description),
				Type:          rawObj.Type,
				Optional:      rawObj.Optional,
			}
			if _, ok := up.Chinese[rawObj.Description]; !ok {
				missingChinese++
			}
			for _, upstreamMapID := range rawObj.Maps {
				if mapID, ok := resolveMapID(up, upstreamMapID); ok {
					obj.Maps = appendUnique(obj.Maps, mapID)
				}
			}
			for zi, zone := range rawObj.Zones {
				mapID, ok := resolveMapID(up, zone.Map)
				if !ok {
					omittedMapReferences++
					continue
				}
				id := zone.ID
				if id == "" {
					id = fmt.Sprintf("%s:zone:%d", rawObj.ID, zi)
				}
				obj.Maps = appendUnique(obj.Maps, mapID)
				obj.Locations = append(obj.Locations, ObjectiveLocation{
					ID: id, MapID: mapID, SourceMapID: zone.Map, Position: zone.Position,
					Outline: zone.Outline, Top: zone.Top, Bottom: zone.Bottom, FloorPending: zone.Top == nil && zone.Bottom == nil,
				})
			}
			for pi, possible := range rawObj.PossibleLocations {
				mapID, ok := resolveMapID(up, possible.Map)
				if !ok {
					omittedMapReferences++
					continue
				}
				obj.Maps = appendUnique(obj.Maps, mapID)
				for li, position := range possible.Positions {
					obj.Locations = append(obj.Locations, ObjectiveLocation{
						ID: fmt.Sprintf("%s:possible:%d:%d", rawObj.ID, pi, li), MapID: mapID,
						SourceMapID: possible.Map, Position: position, FloorPending: true,
					})
				}
			}
			sort.Strings(obj.Maps)
			task.Objectives = append(task.Objectives, obj)
			counts.Objectives++
			counts.Locations += len(obj.Locations)
		}
		mc.Tasks = append(mc.Tasks, task)
	}

	features := map[string]*MapFeature{}
	for key, raw := range up.Maps {
		if raw.ID == "" {
			raw.ID = key
		}
		mapID, ok := resolveMapID(up, raw.ID)
		if !ok {
			continue
		}
		coverage := mc.Coverage[mapID]
		coverage.SourceMapIDs = appendUnique(coverage.SourceMapIDs, raw.ID)
		mc.Coverage[mapID] = coverage
		for _, extract := range raw.Extracts {
			name := translated(up.English, extract.Name)
			faction := normalizeFaction(extract.Faction)
			mergeFeature(features, MapFeature{
				ID:        stableFeatureID(mapID, "extract", name, faction, "", extract.Position),
				SourceIDs: []string{extract.ID}, Kind: "extract", MapID: mapID, SourceMapIDs: []string{raw.ID},
				Name: name, NameZh: translated(up.Chinese, extract.Name),
				Faction: faction, SwitchIDs: rawIDs(extract.Switches),
				TransferItem: rawID(extract.TransferItem), Position: extract.Position, Outline: extract.Outline,
				Top: extract.Top, Bottom: extract.Bottom, FloorPending: extract.Top == nil && extract.Bottom == nil,
			})
		}
		for _, transit := range raw.Transits {
			destination, _ := resolveMapID(up, transit.Map)
			name := translated(up.English, transit.Description)
			mergeFeature(features, MapFeature{
				ID:        stableFeatureID(mapID, "transit", name, "", destination, transit.Position),
				SourceIDs: []string{transit.ID}, Kind: "transit", MapID: mapID, SourceMapIDs: []string{raw.ID},
				Name: name, NameZh: translated(up.Chinese, transit.Description),
				Destination: destination, Position: transit.Position, Outline: transit.Outline,
				Top: transit.Top, Bottom: transit.Bottom, FloorPending: transit.Top == nil && transit.Bottom == nil,
			})
		}
	}
	for _, feature := range features {
		mc.Features = append(mc.Features, *feature)
		coverage := mc.Coverage[feature.MapID]
		if feature.Kind == "extract" {
			coverage.ExtractCount++
			counts.Extracts++
		} else {
			coverage.TransitCount++
			counts.Transits++
		}
		mc.Coverage[feature.MapID] = coverage
	}
	for _, task := range mc.Tasks {
		seenMap := map[string]bool{}
		for _, obj := range task.Objectives {
			for _, mapID := range obj.Maps {
				if !seenMap[mapID] && len(obj.Locations) > 0 {
					coverage := mc.Coverage[mapID]
					coverage.LocatedTasks++
					mc.Coverage[mapID] = coverage
					seenMap[mapID] = true
				}
			}
		}
	}
	for _, id := range supportedMapIDs {
		coverage := mc.Coverage[id]
		sort.Strings(coverage.SourceMapIDs)
		if coverage.ExtractCount+coverage.TransitCount > 0 {
			coverage.Status, coverage.MessageZh = "available", ""
		} else {
			warnings = append(warnings, fmt.Sprintf("%s: %s", id, coverage.MessageZh))
		}
		mc.Coverage[id] = coverage
	}
	counts.Tasks = len(mc.Tasks)
	if missingChinese > 0 {
		warnings = append(warnings, fmt.Sprintf("中文翻译缺失字段：%d", missingChinese))
	}
	if omittedMapReferences > 0 {
		warnings = append(warnings, fmt.Sprintf("尚无受支持底图的坐标引用已忽略：%d", omittedMapReferences))
	}
	sort.Slice(mc.Tasks, func(i, j int) bool { return mc.Tasks[i].ID < mc.Tasks[j].ID })
	sort.Slice(mc.Features, func(i, j int) bool {
		a, b := mc.Features[i], mc.Features[j]
		return a.MapID+":"+a.Kind+":"+a.ID < b.MapID+":"+b.Kind+":"+b.ID
	})
	return mc, counts, warnings
}

func catalogCounts(catalog *ModeCatalog) ModeCounts {
	counts := ModeCounts{}
	if catalog == nil {
		return counts
	}
	for _, task := range catalog.Tasks {
		if task.Retired {
			continue
		}
		counts.Tasks++
		for _, objective := range task.Objectives {
			if objective.Retired {
				continue
			}
			counts.Objectives++
			for _, location := range objective.Locations {
				if !location.Retired {
					counts.Locations++
				}
			}
		}
	}
	for _, feature := range catalog.Features {
		if feature.Kind == "extract" {
			counts.Extracts++
		} else if feature.Kind == "transit" {
			counts.Transits++
		}
	}
	return counts
}

func subtractCounts(current, previous ModeCounts) ModeCounts {
	return ModeCounts{
		Tasks: current.Tasks - previous.Tasks, Objectives: current.Objectives - previous.Objectives,
		Locations: current.Locations - previous.Locations, Extracts: current.Extracts - previous.Extracts,
		Transits: current.Transits - previous.Transits,
	}
}

// preserveKnownGoodData keeps previously verified coordinates when a newer
// upstream snapshot temporarily omits them. Tasks/objectives removed upstream
// remain addressable for progress and regression checks, but are marked retired
// so Catalog.Tasks does not expose them in the active task graph.
func preserveKnownGoodData(current, previous *ModeCatalog) ModeCounts {
	var preserved ModeCounts
	if current == nil || previous == nil {
		return preserved
	}
	currentTasks := make(map[string]int, len(current.Tasks))
	for i := range current.Tasks {
		currentTasks[current.Tasks[i].ID] = i
	}
	for _, oldTask := range previous.Tasks {
		taskIndex, found := currentTasks[oldTask.ID]
		if !found {
			oldTask = cloneTask(oldTask)
			oldTask.Retired = true
			for i := range oldTask.Objectives {
				oldTask.Objectives[i].Retired = true
				preserved.Objectives++
				preserved.Locations += len(oldTask.Objectives[i].Locations)
			}
			current.Tasks = append(current.Tasks, oldTask)
			currentTasks[oldTask.ID] = len(current.Tasks) - 1
			preserved.Tasks++
			continue
		}
		objectives := make(map[string]int, len(current.Tasks[taskIndex].Objectives))
		for i := range current.Tasks[taskIndex].Objectives {
			objectives[current.Tasks[taskIndex].Objectives[i].ID] = i
		}
		for _, oldObjective := range oldTask.Objectives {
			objectiveIndex, found := objectives[oldObjective.ID]
			if !found {
				oldObjective.Maps = append([]string(nil), oldObjective.Maps...)
				oldObjective.Locations = append([]ObjectiveLocation(nil), oldObjective.Locations...)
				oldObjective.Retired = true
				current.Tasks[taskIndex].Objectives = append(current.Tasks[taskIndex].Objectives, oldObjective)
				objectives[oldObjective.ID] = len(current.Tasks[taskIndex].Objectives) - 1
				preserved.Objectives++
				preserved.Locations += len(oldObjective.Locations)
				continue
			}
			objective := &current.Tasks[taskIndex].Objectives[objectiveIndex]
			seenLocations := make(map[string]bool, len(objective.Locations))
			for _, location := range objective.Locations {
				seenLocations[locationKey(location)] = true
			}
			for _, location := range oldObjective.Locations {
				if seenLocations[locationKey(location)] {
					continue
				}
				location.Retired = true
				objective.Locations = append(objective.Locations, location)
				objective.Maps = appendUnique(objective.Maps, location.MapID)
				seenLocations[locationKey(location)] = true
				preserved.Locations++
			}
			sort.Strings(objective.Maps)
		}
	}
	sort.SliceStable(current.Tasks, func(i, j int) bool {
		if current.Tasks[i].Retired != current.Tasks[j].Retired {
			return !current.Tasks[i].Retired
		}
		return false
	})
	return preserved
}

func catalogsCompatible(current, previous *ModeCatalog) bool {
	if current == nil || previous == nil || len(current.Tasks) == 0 || len(previous.Tasks) == 0 {
		return false
	}
	currentIDs := make(map[string]bool, len(current.Tasks))
	for _, task := range current.Tasks {
		currentIDs[task.ID] = true
	}
	activePrevious, overlap := 0, 0
	for _, task := range previous.Tasks {
		if task.Retired {
			continue
		}
		activePrevious++
		if currentIDs[task.ID] {
			overlap++
		}
	}
	return activePrevious > 0 && overlap*2 >= activePrevious
}

func locationKey(location ObjectiveLocation) string {
	// Upstream zone IDs and floating-point precision can change between dumps.
	// World position and internal map are the durable identity. Millimetre
	// rounding absorbs JSON serialization noise while retaining every older
	// coordinate that moved enough to affect the established regression set.
	return fmt.Sprintf("%s\x00%.3f\x00%.3f\x00%.3f",
		location.MapID, location.Position.X, location.Position.Y, location.Position.Z)
}

func mergeFeature(features map[string]*MapFeature, item MapFeature) {
	key := item.MapID + ":" + item.Kind + ":" + item.ID
	if previous := features[key]; previous != nil {
		for _, source := range item.SourceMapIDs {
			previous.SourceMapIDs = appendUnique(previous.SourceMapIDs, source)
		}
		for _, source := range item.SourceIDs {
			previous.SourceIDs = appendUnique(previous.SourceIDs, source)
		}
		sort.Strings(previous.SourceMapIDs)
		sort.Strings(previous.SourceIDs)
		return
	}
	sort.Strings(item.SourceIDs)
	features[key] = &item
}

func stableFeatureID(mapID, kind, name, faction, destination string, position Vec3) string {
	identity := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%.1f\x00%.1f\x00%.1f",
		mapID, kind, strings.ToLower(strings.TrimSpace(name)), faction, destination,
		position.X, position.Y, position.Z)
	sum := sha256.Sum256([]byte(identity))
	return kind + "-" + hex.EncodeToString(sum[:8])
}

func normalizeFaction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pmc", "usec", "bear":
		return "pmc"
	case "scav":
		return "scav"
	case "shared", "all", "both", "any":
		return "shared"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func translated(dict map[string]string, raw string) string {
	if value := strings.TrimSpace(dict[raw]); value != "" {
		return value
	}
	return raw
}

func appendUnique(values []string, item string) []string {
	if item == "" || contains(values, item) {
		return values
	}
	return append(values, item)
}

func rawIDs(values []json.RawMessage) []string {
	var out []string
	for _, value := range values {
		if id := rawID(value); id != "" {
			out = appendUnique(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func rawID(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	var object struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(value, &object)
	return object.ID
}

func packFingerprint(pack *Pack) (string, error) {
	copy := *pack
	copy.Version = ""
	copy.GeneratedAt = time.Time{}
	b, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func writePackAtomic(target string, pack *Pack) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create content directory: %w", err)
	}
	b, err := json.Marshal(pack)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".tarkovmap-content-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	previous := target + ".previous"
	_ = os.Remove(previous)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, previous); err != nil {
			return fmt.Errorf("backup active content pack: %w", err)
		}
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Rename(previous, target)
		return fmt.Errorf("activate content pack: %w", err)
	}
	return nil
}

func decodePackFile(path string) (*Pack, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeAndValidate(b)
}
