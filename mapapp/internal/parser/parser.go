// Screenshot filename parser (PLAN §2.1 / phase 1).
//
// EFT writes screenshots with the player position and camera rotation in
// the file name, e.g.:
//
//	2026-08-19[17-50]_585.73, 11.24, 146.56_0.00000, 0.94890, 0.00000, -0.31557_11.91 (0).png
//	2025-12-20[02-09]-420.18, 1.00, 319.01-0.00089, -0.99307, -0.00012, -0.11748_15.11 (0).png
//
// Layout: date[time]_v1, v2, v3_qx, qy, qz, qw_suffix.png
// The separator between the time/coords/quat groups is "_" in current game
// versions; in the older sample above the leading "-" of negative numbers
// served as the separator, so both forms are accepted.
// The axis meaning of v1..v3 is NOT assumed here — Parse returns the raw
// triplet, and Remap applies the configured axis order.
package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var filenameRe = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2})\[(\d{2}-\d{2})\]` +
		`_?(-?\d+(?:\.\d+)?),\s*(-?\d+(?:\.\d+)?),\s*(-?\d+(?:\.\d+)?)` +
		`_?(-?\d+(?:\.\d+)?),\s*(-?\d+(?:\.\d+)?),\s*(-?\d+(?:\.\d+)?),\s*(-?\d+(?:\.\d+)?)` +
		`_(.*)\.png$`)

type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type Quat struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
	W float64 `json:"w"`
}

type Position struct {
	Date     string `json:"date"`
	Time     string `json:"time"`
	World    Vec3   `json:"world"`
	Rotation Quat   `json:"rotation"`
	Suffix   string `json:"suffix"`
	Raw      string `json:"rawFilename"`
}

// Parse extracts the raw values. The coordinate triplet is stored in the
// order it appears in the filename; use Remap to apply an axis order.
func Parse(filename string) (*Position, error) {
	name := filename
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	m := filenameRe.FindStringSubmatch(name)
	if m == nil {
		return nil, fmt.Errorf("not an EFT screenshot filename: %q", name)
	}
	nums := make([]float64, 7)
	for i := 0; i < 7; i++ {
		v, err := strconv.ParseFloat(m[3+i], 64)
		if err != nil {
			return nil, fmt.Errorf("bad number %q in %q: %w", m[3+i], name, err)
		}
		nums[i] = v
	}
	return &Position{
		Date:     m[1],
		Time:     m[2],
		World:    Vec3{X: nums[0], Y: nums[1], Z: nums[2]}, // raw order, see Remap
		Rotation: Quat{X: nums[3], Y: nums[4], Z: nums[5], W: nums[6]},
		Suffix:   m[10],
		Raw:      name,
	}, nil
}

// Remap reorders the raw triplet according to axisOrder (e.g. "x,y,z"
// means the file already holds x first; "y,z,x" means file order is y,z,x).
func (p *Position) Remap(axisOrder string) error {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(axisOrder)), ",")
	if len(parts) != 3 {
		return fmt.Errorf("bad axisOrder %q: want 3 axes", axisOrder)
	}
	raw := []float64{p.World.X, p.World.Y, p.World.Z}
	out := map[string]float64{}
	for i, axis := range parts {
		switch axis {
		case "x", "y", "z":
			out[axis] = raw[i]
		default:
			return fmt.Errorf("bad axisOrder %q: unknown axis %q", axisOrder, axis)
		}
	}
	if len(out) != 3 {
		return fmt.Errorf("bad axisOrder %q: axes must be unique", axisOrder)
	}
	p.World = Vec3{X: out["x"], Y: out["y"], Z: out["z"]}
	return nil
}
