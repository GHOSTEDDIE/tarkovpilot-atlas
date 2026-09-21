// Map preset name resolution (PLAN §2.3): raw names from game logs ->
// internal map IDs. PVP logs `location: <name>,`, PVE logs
// `path:maps/<name>.bundle`; both funnel through Resolve.
package gamelogs

import "strings"

// aliases maps lowercased log names to registry map IDs.
// Based on SPT location reference; extend as the game adds presets.
var aliases = map[string]string{
	"bigmap":                 "customs",
	"customs":                "customs",
	"factory4_day":           "factory",
	"factory4_night":         "factory",
	"factory":                "factory",
	"woods":                  "woods",
	"shoreline":              "shoreline",
	"interchange":            "interchange",
	"rezervbase":             "reserve",
	"reserve":                "reserve",
	"laboratory":             "lab",
	"laboratory_dark_preset": "lab",
	"lab":                    "lab",
	"lighthouse":             "lighthouse",
	"tarkovstreets":          "streetsoftarkov",
	"streetsoftarkov":        "streetsoftarkov",
	"sandbox":                "groundzero",
	"sandbox_high":           "groundzero",
	"sandbox_start_preset":   "groundzero",
	"groundzero":             "groundzero",
	"labyrinth":              "labyrinth",
	"labyrinth_preset":       "labyrinth",
	"icebreaker":             "icebreaker",
	"terminal":               "terminal",
}

// Resolve converts a raw log location name to a registry map ID.
// ok=false for unknown presets — callers must not guess (PLAN §2.3).
func Resolve(raw string) (id string, ok bool) {
	id, ok = aliases[strings.ToLower(strings.TrimSpace(raw))]
	return id, ok
}
