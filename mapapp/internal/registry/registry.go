// Map registry: embedded metadata for all supported EFT maps.
//
// The base data comes from TarkovTracker/tarkovdata maps.json (bounds,
// floors, coordinateRotation). Projection settings are starting values —
// they are meant to be refined by in-game calibration (see PLAN §4.2).
package registry

import (
	_ "embed"
	"encoding/json"
)

//go:embed maps.json
var mapsJSON []byte

type Bounds struct {
	MinX float64 `json:"minX"`
	MaxX float64 `json:"maxX"`
	MinZ float64 `json:"minZ"`
	MaxZ float64 `json:"maxZ"`
}

// Affine is a 2D affine transform [a b c d e f]:
//
//	x' = a*x + b*y + c
//	y' = d*x + e*y + f
type Affine [6]float64

func IdentityAffine() Affine { return Affine{1, 0, 0, 0, 1, 0} }

func (af Affine) Apply(x, y float64) (float64, float64) {
	return af[0]*x + af[1]*y + af[2], af[3]*x + af[4]*y + af[5]
}

type Projection struct {
	// AxisOrder maps the three numbers in a screenshot filename onto x,y,z
	// (PLAN §2.2: must stay configurable until verified in-game).
	AxisOrder      string   `json:"axisOrder"`
	HorizontalAxes []string `json:"horizontalAxes"` // which axes form the map plane
	MirrorX        bool     `json:"mirrorX"`
	MirrorY        bool     `json:"mirrorY"`
	Rotation       float64  `json:"rotation"` // degrees, applied to the horizontal plane
	// Affine refines the normalized projection after calibration and maps
	// world (horizontal) coordinates directly to SVG pixel coordinates.
	Affine            Affine  `json:"affine"`
	Calibrated        bool    `json:"calibrated"`
	CalibrationError  float64 `json:"calibrationError"` // RMS error in SVG pixels
	CalibrationPoints int     `json:"calibrationPoints"`
}

type FloorRange struct {
	Floor string  `json:"floor"`
	MinY  float64 `json:"minY"`
	MaxY  float64 `json:"maxY"`
}

// Raster describes a locally bundled slippy-map tile set. Transform matches
// Leaflet's CRS.Simple transformation [scaleX, marginX, scaleY, marginY]; the
// vertical scale is inverted when world coordinates are projected to pixels.
type Raster struct {
	Zoom      int           `json:"zoom"`
	TileSize  int           `json:"tileSize"`
	Transform [4]float64    `json:"transform"`
	Layers    []RasterLayer `json:"layers"`
}

type RasterLayer struct {
	Floor     string `json:"floor"`
	TilePath  string `json:"tilePath"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

type Map struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	NameRu                 string       `json:"nameRu"`
	NameZh                 string       `json:"nameZh"`
	SvgFile                string       `json:"svgFile"`
	FallbackImage          string       `json:"fallbackImage,omitempty"`
	FallbackImageSource    string       `json:"fallbackImageSource,omitempty"`
	FallbackImageWidth     int          `json:"fallbackImageWidth,omitempty"`
	FallbackImageHeight    int          `json:"fallbackImageHeight,omitempty"`
	FallbackAttribution    string       `json:"fallbackAttribution,omitempty"`
	FallbackAttributionURL string       `json:"fallbackAttributionUrl,omitempty"`
	Floors                 []string     `json:"floors"`
	DefaultFloor           string       `json:"defaultFloor"`
	Projection             Projection   `json:"projection"`
	Bounds                 Bounds       `json:"bounds"`
	FloorRanges            []FloorRange `json:"floorRanges"`
	Raster                 *Raster      `json:"raster,omitempty"`
	Attribution            string       `json:"attribution,omitempty"`
	AttributionURL         string       `json:"attributionUrl,omitempty"`
}

// FloorForY picks a floor by world height; empty = caller uses default.
func (m *Map) FloorForY(y float64) string {
	for _, fr := range m.FloorRanges {
		if y >= fr.MinY && y < fr.MaxY {
			return fr.Floor
		}
	}
	return ""
}

type Registry struct {
	Maps map[string]*Map
}

func Load() (*Registry, error) {
	raw := map[string]*Map{}
	if err := json.Unmarshal(mapsJSON, &raw); err != nil {
		return nil, err
	}
	for id, m := range raw {
		m.ID = id
	}
	return &Registry{Maps: raw}, nil
}

func MustLoad() *Registry {
	r, err := Load()
	if err != nil {
		panic(err)
	}
	return r
}

func (r *Registry) Get(id string) *Map { return r.Maps[id] }

// Clone returns a deep copy (via JSON) so callers can mutate safely.
func (m *Map) Clone() *Map {
	b, _ := json.Marshal(m)
	var c Map
	_ = json.Unmarshal(b, &c)
	return &c
}
