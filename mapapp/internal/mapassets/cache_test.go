package mapassets

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tarkovmap/internal/registry"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestRefreshActivatesOnlyCompleteValidatedPackage(t *testing.T) {
	pngBytes := testPNG(t)
	jpegBytes := testJPEG(t, 8, 12)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body []byte
		switch {
		case r.URL.Path == "/svg/Test.svg":
			body = []byte(`<svg viewBox="0 0 10 10"><g id="Ground_Level"/></svg>`)
		case r.URL.Path == "/reference/test.jpg":
			body = jpegBytes
		case strings.HasPrefix(r.URL.Path, "/tiles/"):
			body = pngBytes
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(bytes.NewReader(nil)), Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}

	reg := fixtureRegistry("https://fixture.invalid")
	root := t.TempDir()
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	report, err := Refresh(context.Background(), reg, RefreshOptions{
		Root: root, SVGBaseURL: "https://fixture.invalid/svg/", Client: client, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.Files != 6 || report.Version == "" {
		t.Fatalf("unexpected report: %#v", report)
	}
	directory, version, err := LoadActive(root, reg)
	if err != nil || version != report.Version || directory != report.Directory {
		t.Fatalf("active package = %q %q %v", directory, version, err)
	}

	second, err := Refresh(context.Background(), reg, RefreshOptions{
		Root: root, SVGBaseURL: "https://fixture.invalid/svg/", Client: client, Now: func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil || !second.Unchanged || second.Applied {
		t.Fatalf("unchanged refresh = %#v, %v", second, err)
	}
}

func TestRefreshFailureKeepsLastKnownGoodPackage(t *testing.T) {
	pngBytes := testPNG(t)
	jpegBytes := testJPEG(t, 8, 12)
	broken := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body []byte
		if r.URL.Path == "/svg/Test.svg" {
			if broken {
				body = []byte("not-svg")
			} else {
				body = []byte(`<svg viewBox="0 0 10 10"><g id="Ground_Level"/></svg>`)
			}
		} else if r.URL.Path == "/reference/test.jpg" {
			body = jpegBytes
		} else {
			body = pngBytes
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	reg := fixtureRegistry("https://fixture.invalid")
	root := t.TempDir()
	first, err := Refresh(context.Background(), reg, RefreshOptions{Root: root, SVGBaseURL: "https://fixture.invalid/svg/", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	broken = true
	if _, err := Refresh(context.Background(), reg, RefreshOptions{Root: root, SVGBaseURL: "https://fixture.invalid/svg/", Client: client}); err == nil {
		t.Fatal("damaged package unexpectedly activated")
	}
	_, version, err := LoadActive(root, reg)
	if err != nil || version != first.Version {
		t.Fatalf("last-known-good package changed: %q, %v", version, err)
	}
	if _, err := os.Stat(filepath.Join(root, "active.json")); err != nil {
		t.Fatal(err)
	}
}

func fixtureRegistry(base string) *registry.Registry {
	return &registry.Registry{Maps: map[string]*registry.Map{
		"test": {
			ID: "test", SvgFile: "Test.svg", Floors: []string{"Ground_Level"},
			FallbackImage: "reference/test.jpg", FallbackImageSource: base + "/reference/test.jpg",
			FallbackImageWidth: 8, FallbackImageHeight: 12,
			Raster: &registry.Raster{Zoom: 1, TileSize: 2, Layers: []registry.RasterLayer{{
				Floor: "Ground_Level", TilePath: "tiles/test/{z}/{x}-{y}.png", SourceURL: base + "/tiles/{z}/{x}/{y}.png",
			}}},
		},
	}}
}

func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, nil); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
