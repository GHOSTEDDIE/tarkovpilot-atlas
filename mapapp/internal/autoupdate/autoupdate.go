// Package autoupdate schedules content and map-art refreshes without blocking
// server startup. Update callbacks must activate data atomically themselves.
package autoupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Status struct {
	Enabled         bool      `json:"enabled"`
	Checking        bool      `json:"checking"`
	LastAttempt     time.Time `json:"lastAttempt,omitempty"`
	LastSuccess     time.Time `json:"lastSuccess,omitempty"`
	NextCheck       time.Time `json:"nextCheck,omitempty"`
	ContentVersion  string    `json:"contentVersion,omitempty"`
	MapAssetVersion string    `json:"mapAssetVersion,omitempty"`
	ContentError    string    `json:"contentError,omitempty"`
	MapAssetError   string    `json:"mapAssetError,omitempty"`
}

type Config struct {
	StateFile      string
	Interval       time.Duration
	RetryInterval  time.Duration
	Force          bool
	ContentUpdate  func(context.Context) (string, error)
	MapAssetUpdate func(context.Context) (string, error)
	OnStatus       func(Status)
	Now            func() time.Time
}

func Load(path string) (Status, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(b, &status); err != nil {
		return Status{}, fmt.Errorf("decode update state: %w", err)
	}
	status.Checking = false
	return status, nil
}

func Save(path string, status Status) error {
	if path == "" {
		return errors.New("update state file is required")
	}
	b, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-state-*.json")
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
	return os.Rename(tmpName, path)
}

func CheckOnce(ctx context.Context, cfg Config, status Status) Status {
	now := time.Now().UTC()
	if cfg.Now != nil {
		now = cfg.Now().UTC()
	}
	status.Enabled = true
	status.Checking = true
	status.LastAttempt = now
	status.ContentError = ""
	status.MapAssetError = ""
	if cfg.OnStatus != nil {
		cfg.OnStatus(status)
	}

	type result struct {
		kind    string
		version string
		err     error
	}
	results := make(chan result, 2)
	var updates sync.WaitGroup
	run := func(kind string, fn func(context.Context) (string, error)) {
		defer updates.Done()
		if fn == nil {
			results <- result{kind: kind}
			return
		}
		version, err := fn(ctx)
		results <- result{kind: kind, version: version, err: err}
	}
	updates.Add(2)
	go run("content", cfg.ContentUpdate)
	go run("maps", cfg.MapAssetUpdate)
	go func() {
		updates.Wait()
		close(results)
	}()
	for item := range results {
		switch item.kind {
		case "content":
			if item.err != nil {
				status.ContentError = item.err.Error()
			} else if item.version != "" {
				status.ContentVersion = item.version
			}
		case "maps":
			if item.err != nil {
				status.MapAssetError = item.err.Error()
			} else if item.version != "" {
				status.MapAssetVersion = item.version
			}
		}
	}
	status.Checking = false
	if status.ContentError == "" && status.MapAssetError == "" {
		status.LastSuccess = now
		status.NextCheck = now.Add(normalizeInterval(cfg.Interval, 24*time.Hour))
	} else {
		status.NextCheck = now.Add(normalizeInterval(cfg.RetryInterval, time.Hour))
	}
	return status
}

func Run(ctx context.Context, cfg Config) {
	status, err := Load(cfg.StateFile)
	if err != nil {
		status = Status{ContentError: err.Error()}
	}
	status.Enabled = true
	if cfg.Force {
		status.NextCheck = time.Time{}
	}
	for {
		now := time.Now().UTC()
		if cfg.Now != nil {
			now = cfg.Now().UTC()
		}
		if status.NextCheck.IsZero() || !status.NextCheck.After(now) {
			status = CheckOnce(ctx, cfg, status)
			_ = Save(cfg.StateFile, status)
			if cfg.OnStatus != nil {
				cfg.OnStatus(status)
			}
		}
		wait := time.Until(status.NextCheck)
		if cfg.Now != nil {
			wait = status.NextCheck.Sub(cfg.Now().UTC())
		}
		if wait < time.Second {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func normalizeInterval(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}
