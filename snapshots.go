package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	automaticSnapshotInterval  = 24 * time.Hour
	automaticSnapshotRetention = 14
)

// CreateAutomaticSnapshot copies the validated append-only event log to a
// timestamped, owner-readable file. It never modifies the live log. A new copy
// is made only when the log changed and the newest snapshot is at least one day
// old; the newest fourteen snapshots are retained.
func (s *Store) CreateAutomaticSnapshot(now time.Time) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sourceInfo, err := os.Stat(s.eventPath)
	if errors.Is(err, os.ErrNotExist) || (err == nil && sourceInfo.Size() == 0) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect event log for snapshot: %w", err)
	}

	directory := filepath.Join(s.dataDir, "snapshots")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", false, fmt.Errorf("create snapshot directory: %w", err)
	}
	entries, err := snapshotFiles(directory)
	if err != nil {
		return "", false, err
	}
	if len(entries) > 0 {
		latest := entries[len(entries)-1]
		if now.Sub(latest.modified) < automaticSnapshotInterval {
			return "", false, nil
		}
		unchanged, err := filesHaveSameContents(s.eventPath, latest.path)
		if err != nil {
			return "", false, fmt.Errorf("compare event log with latest snapshot: %w", err)
		}
		if unchanged {
			return "", false, nil
		}
	}

	source, err := os.Open(s.eventPath)
	if err != nil {
		return "", false, fmt.Errorf("open event log for snapshot: %w", err)
	}
	defer source.Close()
	temporary, err := os.CreateTemp(directory, ".events-snapshot-*")
	if err != nil {
		return "", false, fmt.Errorf("create temporary snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", false, fmt.Errorf("protect temporary snapshot: %w", err)
	}
	written, copyErr := io.Copy(temporary, source)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", false, fmt.Errorf("copy event log snapshot: %w", copyErr)
	}
	if syncErr != nil {
		return "", false, fmt.Errorf("sync event log snapshot: %w", syncErr)
	}
	if closeErr != nil {
		return "", false, fmt.Errorf("close event log snapshot: %w", closeErr)
	}
	if written != sourceInfo.Size() {
		return "", false, fmt.Errorf("snapshot size changed during copy")
	}
	filename := "events-" + now.UTC().Format("20060102T150405Z") + ".ndjson"
	destination := filepath.Join(directory, filename)
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", false, fmt.Errorf("publish event log snapshot: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return "", false, err
	}
	if err := pruneSnapshots(directory, automaticSnapshotRetention); err != nil {
		return destination, true, err
	}
	return destination, true, nil
}

func filesHaveSameContents(firstPath, secondPath string) (bool, error) {
	first, err := os.Open(firstPath)
	if err != nil {
		return false, err
	}
	defer first.Close()
	second, err := os.Open(secondPath)
	if err != nil {
		return false, err
	}
	defer second.Close()

	firstHash := sha256.New()
	if _, err := io.Copy(firstHash, first); err != nil {
		return false, err
	}
	secondHash := sha256.New()
	if _, err := io.Copy(secondHash, second); err != nil {
		return false, err
	}
	return bytes.Equal(firstHash.Sum(nil), secondHash.Sum(nil)), nil
}

type snapshotFile struct {
	path     string
	modified time.Time
}

func snapshotFiles(directory string) ([]snapshotFile, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read snapshot directory: %w", err)
	}
	files := make([]snapshotFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "events-") || !strings.HasSuffix(entry.Name(), ".ndjson") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect snapshot %s: %w", entry.Name(), err)
		}
		files = append(files, snapshotFile{path: filepath.Join(directory, entry.Name()), modified: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.Before(files[j].modified) })
	return files, nil
}

func pruneSnapshots(directory string, retain int) error {
	files, err := snapshotFiles(directory)
	if err != nil {
		return err
	}
	for len(files) > retain {
		if err := os.Remove(files[0].path); err != nil {
			return fmt.Errorf("remove expired snapshot: %w", err)
		}
		files = files[1:]
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open snapshot directory: %w", err)
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return fmt.Errorf("inspect snapshot directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("snapshot path is not a directory")
	}
	// Windows does not support File.Sync on a directory opened by os.Open.
	// Callers still sync and close file contents before publishing them. Keep
	// directory fsync on Unix, where it also makes rename metadata durable.
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync snapshot directory: %w", err)
	}
	return nil
}
