package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAutomaticEventLogSnapshots(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if path, created, err := store.CreateAutomaticSnapshot(time.Now().UTC()); err != nil || created || path != "" {
		t.Fatalf("empty log snapshot = %q, %v, %v", path, created, err)
	}
	if err := store.SaveAPRSPasscode("12345"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateResponder(ResponderInput{Name: "Snapshot Unit", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	path, created, err := store.CreateAutomaticSnapshot(now)
	if err != nil || !created {
		t.Fatalf("first snapshot = %q, %v, %v", path, created, err)
	}
	events, err := os.ReadFile(filepath.Join(dataDir, "events.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(events, snapshot) {
		t.Fatal("snapshot does not exactly match the event log")
	}
	if bytes.Contains(snapshot, []byte("12345")) || bytes.Contains(snapshot, []byte("passcode")) {
		t.Fatal("snapshot contains an APRS credential")
	}
	// Windows uses inherited directory ACLs, not POSIX permission bits.
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("snapshot permissions = %v, %v", info, err)
	}
	if next, created, err := store.CreateAutomaticSnapshot(now.Add(time.Hour)); err != nil || created || next != "" {
		t.Fatalf("snapshot repeated too soon = %q, %v, %v", next, created, err)
	}
	if _, err := store.CreateResponder(ResponderInput{Name: "Changed Unit", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	// Some CI filesystems expose timestamps too coarsely to distinguish the
	// appended event log from the snapshot created moments earlier.
	latestInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dataDir, "events.ndjson"), latestInfo.ModTime(), latestInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	path, created, err = store.CreateAutomaticSnapshot(now.Add(25 * time.Hour))
	if err != nil || !created {
		t.Fatalf("changed daily snapshot = %q, %v, %v", path, created, err)
	}
	files, err := snapshotFiles(filepath.Join(dataDir, "snapshots"))
	if err != nil || len(files) != 2 {
		t.Fatalf("snapshot count = %d, %v", len(files), err)
	}
}

func TestSyncDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := syncDirectory(directory); err != nil {
		t.Fatal(err)
	}
	if err := syncDirectory(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
	file := filepath.Join(directory, "regular-file")
	if err := os.WriteFile(file, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncDirectory(file); err == nil {
		t.Fatal("regular file accepted as a directory")
	}
}

func TestSnapshotRetention(t *testing.T) {
	directory := t.TempDir()
	for index := 0; index < automaticSnapshotRetention+3; index++ {
		path := filepath.Join(directory, "events-20260908T1200"+twoDigits(index)+"Z.ndjson")
		if err := os.WriteFile(path, []byte("event\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(index+1), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneSnapshots(directory, automaticSnapshotRetention); err != nil {
		t.Fatal(err)
	}
	files, err := snapshotFiles(directory)
	if err != nil || len(files) != automaticSnapshotRetention {
		t.Fatalf("retained snapshots = %d, %v", len(files), err)
	}
}

func twoDigits(value int) string {
	return string([]byte{'0' + byte(value/10), '0' + byte(value%10)})
}
