// Package mapassets manages versioned runtime map-art packages. The embedded
// maps remain the final fallback; downloaded packages are activated only after
// every SVG and raster tile has passed validation.
package mapassets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tarkovmap/internal/registry"
)

const DefaultSVGBaseURL = "https://assets.tarkov.dev/maps/svg/"

type RefreshOptions struct {
	Root       string
	SVGBaseURL string
	Client     *http.Client
	Now        func() time.Time
}

type Report struct {
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generatedAt"`
	Directory   string    `json:"directory"`
	Files       int       `json:"files"`
	Bytes       int64     `json:"bytes"`
	SHA256      string    `json:"sha256"`
	Applied     bool      `json:"applied"`
	Unchanged   bool      `json:"unchanged"`
}

type activeManifest struct {
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generatedAt"`
	Files       int       `json:"files"`
	Bytes       int64     `json:"bytes"`
	SHA256      string    `json:"sha256"`
}

type asset struct {
	URL    string
	Path   string
	Kind   string
	Floors []string
	Width  int
	Height int
}

func LoadActive(root string, reg *registry.Registry) (directory, version string, err error) {
	if root == "" {
		return "", "", errors.New("map asset root is required")
	}
	b, err := os.ReadFile(filepath.Join(root, "active.json"))
	if err != nil {
		return "", "", err
	}
	var manifest activeManifest
	if err := json.Unmarshal(b, &manifest); err != nil || manifest.Version == "" {
		return "", "", fmt.Errorf("decode active map assets: %w", err)
	}
	directory = filepath.Join(root, "versions", manifest.Version)
	assets, err := expectedAssets(reg, DefaultSVGBaseURL)
	if err != nil {
		return "", "", err
	}
	if _, _, err := validatePackage(directory, assets); err != nil {
		return "", "", fmt.Errorf("validate active map assets: %w", err)
	}
	return directory, manifest.Version, nil
}

func Refresh(ctx context.Context, reg *registry.Registry, opts RefreshOptions) (*Report, error) {
	if reg == nil {
		return nil, errors.New("map registry is required")
	}
	if opts.Root == "" {
		return nil, errors.New("map asset root is required")
	}
	svgBase := strings.TrimRight(opts.SVGBaseURL, "/") + "/"
	if strings.TrimSpace(opts.SVGBaseURL) == "" {
		svgBase = DefaultSVGBaseURL
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	assets, err := expectedAssets(reg, svgBase)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(opts.Root, "versions"), 0o755); err != nil {
		return nil, fmt.Errorf("create map asset cache: %w", err)
	}
	staging, err := os.MkdirTemp(opts.Root, ".staging-")
	if err != nil {
		return nil, fmt.Errorf("create map asset staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	jobs := make(chan asset)
	errCh := make(chan error, 1)
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 4 {
		workerCount = 4
	}
	if workerCount > 16 {
		workerCount = 16
	}
	var workers sync.WaitGroup
	worker := func() {
		defer workers.Done()
		for item := range jobs {
			if err := downloadAsset(ctx, client, staging, item); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}
	}
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}
	for _, item := range assets {
		select {
		case jobs <- item:
		case err := <-errCh:
			close(jobs)
			workers.Wait()
			return nil, err
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()
	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	hash, size, err := validatePackage(staging, assets)
	if err != nil {
		return nil, err
	}
	version := hash[:16]
	report := &Report{
		Version: version, GeneratedAt: now, Files: len(assets), Bytes: size, SHA256: hash,
	}
	if _, activeVersion, err := LoadActive(opts.Root, reg); err == nil && activeVersion == version {
		report.Directory = filepath.Join(opts.Root, "versions", version)
		report.Unchanged = true
		return report, nil
	}
	destination := filepath.Join(opts.Root, "versions", version)
	if _, err := os.Stat(destination); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(staging, destination); err != nil {
			return nil, fmt.Errorf("activate staged map asset directory: %w", err)
		}
		staging = ""
	} else if err == nil {
		existingHash, _, validateErr := validatePackage(destination, assets)
		if validateErr != nil || existingHash != hash {
			return nil, fmt.Errorf("existing map asset version %s is invalid: %v", version, validateErr)
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect map asset version: %w", err)
	}
	manifest := activeManifest{
		Version: version, GeneratedAt: now, Files: len(assets), Bytes: size, SHA256: hash,
	}
	if err := writeManifestAtomic(filepath.Join(opts.Root, "active.json"), manifest); err != nil {
		return nil, err
	}
	report.Directory = destination
	report.Applied = true
	return report, nil
}

func expectedAssets(reg *registry.Registry, svgBase string) ([]asset, error) {
	ids := make([]string, 0, len(reg.Maps))
	for id := range reg.Maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []asset
	for _, id := range ids {
		m := reg.Maps[id]
		if m.SvgFile != "" {
			if err := validateRelativePath(m.SvgFile); err != nil {
				return nil, fmt.Errorf("map %s SVG path: %w", id, err)
			}
			out = append(out, asset{URL: svgBase + m.SvgFile, Path: m.SvgFile, Kind: "svg", Floors: m.Floors})
		}
		if m.FallbackImage != "" {
			if err := validateRelativePath(m.FallbackImage); err != nil {
				return nil, fmt.Errorf("map %s fallback image path: %w", id, err)
			}
			if m.FallbackImageSource == "" {
				return nil, fmt.Errorf("map %s fallback image has no source URL", id)
			}
			if m.FallbackImageWidth <= 0 || m.FallbackImageHeight <= 0 {
				return nil, fmt.Errorf("map %s fallback image dimensions are invalid", id)
			}
			out = append(out, asset{
				URL: m.FallbackImageSource, Path: m.FallbackImage, Kind: "jpeg",
				Width: m.FallbackImageWidth, Height: m.FallbackImageHeight,
			})
		}
		if m.Raster == nil {
			continue
		}
		for _, layer := range m.Raster.Layers {
			if layer.SourceURL == "" {
				return nil, fmt.Errorf("map %s raster layer %s has no source URL", id, layer.Floor)
			}
			for x := 0; x < 1<<m.Raster.Zoom; x++ {
				for y := 0; y < 1<<m.Raster.Zoom; y++ {
					replacer := strings.NewReplacer(
						"{z}", strconv.Itoa(m.Raster.Zoom), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y),
					)
					path := replacer.Replace(layer.TilePath)
					if err := validateRelativePath(path); err != nil {
						return nil, fmt.Errorf("map %s tile path: %w", id, err)
					}
					out = append(out, asset{URL: replacer.Replace(layer.SourceURL), Path: path, Kind: "png"})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func downloadAsset(ctx context.Context, client *http.Client, root string, item asset) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", item.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", item.URL, resp.Status)
	}
	limit := int64(8 << 20)
	if item.Kind == "svg" {
		limit = 32 << 20
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", item.URL, err)
	}
	if int64(len(b)) > limit {
		return fmt.Errorf("asset %s exceeds %d bytes", item.URL, limit)
	}
	if err := validateBytes(item, b); err != nil {
		return fmt.Errorf("validate %s: %w", item.URL, err)
	}
	target := filepath.Join(root, filepath.FromSlash(item.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

func validatePackage(root string, assets []asset) (string, int64, error) {
	h := sha256.New()
	var size int64
	for _, item := range assets {
		path := filepath.Join(root, filepath.FromSlash(item.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			return "", 0, fmt.Errorf("read map asset %s: %w", item.Path, err)
		}
		if err := validateBytes(item, b); err != nil {
			return "", 0, fmt.Errorf("validate map asset %s: %w", item.Path, err)
		}
		_, _ = h.Write([]byte(item.Path))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		size += int64(len(b))
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func validateBytes(item asset, b []byte) error {
	if len(b) == 0 {
		return errors.New("empty asset")
	}
	switch item.Kind {
	case "svg":
		lower := bytes.ToLower(b)
		if !bytes.Contains(lower, []byte("<svg")) || !bytes.Contains(lower, []byte("viewbox=")) {
			return errors.New("invalid SVG document")
		}
		for _, floor := range item.Floors {
			quotedDouble := []byte(`id="` + floor + `"`)
			quotedSingle := []byte(`id='` + floor + `'`)
			if !bytes.Contains(b, quotedDouble) && !bytes.Contains(b, quotedSingle) {
				return fmt.Errorf("SVG floor %s missing", floor)
			}
		}
	case "png", "jpeg":
		config, format, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || format != item.Kind {
			return fmt.Errorf("invalid %s: %w", strings.ToUpper(item.Kind), err)
		}
		if config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 {
			return fmt.Errorf("invalid %s dimensions %dx%d", strings.ToUpper(item.Kind), config.Width, config.Height)
		}
		if item.Width > 0 && (config.Width != item.Width || config.Height != item.Height) {
			return fmt.Errorf("unexpected %s dimensions %dx%d, want %dx%d", strings.ToUpper(item.Kind), config.Width, config.Height, item.Width, item.Height)
		}
	default:
		return fmt.Errorf("unsupported asset type %q", item.Kind)
	}
	return nil
}

func validateRelativePath(value string) error {
	clean := filepath.Clean(filepath.FromSlash(value))
	if value == "" || filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return fmt.Errorf("unsafe relative path %q", value)
	}
	return nil
}

func writeManifestAtomic(path string, manifest activeManifest) error {
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".active-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("activate map asset manifest: %w", err)
	}
	return nil
}
