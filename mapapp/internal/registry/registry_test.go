package registry

import "testing"

func TestLatestRasterMapsAreRegistered(t *testing.T) {
	reg := MustLoad()
	for id, wantFloors := range map[string]int{"labyrinth": 1, "icebreaker": 16} {
		m := reg.Get(id)
		if m == nil || m.Raster == nil {
			t.Fatalf("%s raster map is not registered: %#v", id, m)
		}
		if len(m.Floors) != wantFloors || len(m.Raster.Layers) != wantFloors {
			t.Fatalf("%s floors=%d layers=%d, want %d", id, len(m.Floors), len(m.Raster.Layers), wantFloors)
		}
		if m.Raster.Zoom != 2 || m.Raster.TileSize != 256 {
			t.Fatalf("%s raster metadata is invalid: %#v", id, m.Raster)
		}
		for _, layer := range m.Raster.Layers {
			if layer.Floor == "" || layer.TilePath == "" {
				t.Fatalf("%s has incomplete raster layer: %#v", id, layer)
			}
		}
	}
}

func TestLatestVariantFloorResolution(t *testing.T) {
	reg := MustLoad()
	if got := reg.Get("icebreaker").FloorForY(34.8); got != "Officers_Deck" {
		t.Fatalf("icebreaker floor at y=34.8 = %q", got)
	}
	if got := reg.Get("labyrinth").FloorForY(0); got != "Main_Level" {
		t.Fatalf("labyrinth floor at y=0 = %q", got)
	}
}

func TestLighthouseStaticFallbackIsRegistered(t *testing.T) {
	m := MustLoad().Get("lighthouse")
	if m.FallbackImage != "reference/lighthouse-2d.jpg" || m.FallbackImageSource == "" {
		t.Fatalf("lighthouse fallback is incomplete: %#v", m)
	}
	if m.FallbackImageWidth != 2242 || m.FallbackImageHeight != 3892 {
		t.Fatalf("lighthouse fallback dimensions = %dx%d", m.FallbackImageWidth, m.FallbackImageHeight)
	}
	if m.FallbackAttribution == "" || m.FallbackAttributionURL == "" {
		t.Fatalf("lighthouse fallback attribution is missing: %#v", m)
	}
}
