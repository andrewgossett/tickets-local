package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxRestoreBytes = 32 << 20

type RestoreReport struct {
	SHA256         string `json:"sha256"`
	Bytes          int    `json:"bytes"`
	Incidents      int    `json:"incidents"`
	Responders     int    `json:"responders"`
	Facilities     int    `json:"facilities"`
	Locations      int    `json:"locations"`
	Overlays       int    `json:"overlays"`
	Activity       int    `json:"activity"`
	AttachmentRefs int    `json:"attachment_refs"`
	Message        string `json:"message"`
}

func ValidateRestore(data []byte) (State, RestoreReport, error) {
	if len(data) == 0 {
		return State{}, RestoreReport{}, validationError{"select a non-empty NDJSON event log"}
	}
	if len(data) > maxRestoreBytes {
		return State{}, RestoreReport{}, validationError{"event log must be 32 MB or smaller"}
	}
	directory, err := os.MkdirTemp("", "tickets-local-restore-validate-")
	if err != nil {
		return State{}, RestoreReport{}, fmt.Errorf("prepare restore validation: %w", err)
	}
	defer os.RemoveAll(directory)
	if err := os.WriteFile(filepath.Join(directory, "events.ndjson"), data, 0o600); err != nil {
		return State{}, RestoreReport{}, fmt.Errorf("prepare restore validation: %w", err)
	}
	store, err := OpenStore(directory)
	if err != nil {
		return State{}, RestoreReport{}, validationError{"event log validation failed: " + err.Error()}
	}
	state := store.Snapshot()
	report := RestoreReport{}
	for _, incident := range state.Incidents {
		if incident.ID == "" || filepath.Base(incident.ID) != incident.ID {
			return State{}, RestoreReport{}, validationError{"event log contains an unsafe incident identifier"}
		}
		for _, attachment := range incident.Attachments {
			report.AttachmentRefs++
			if attachment.StorageName == "" || filepath.Base(attachment.StorageName) != attachment.StorageName || strings.ContainsAny(attachment.StorageName, `/\\`) {
				return State{}, RestoreReport{}, validationError{"event log contains an unsafe attachment reference"}
			}
		}
	}
	digest := sha256.Sum256(data)
	report.SHA256 = hex.EncodeToString(digest[:])
	report.Bytes = len(data)
	report.Incidents = len(state.Incidents)
	report.Responders = len(state.Responders)
	report.Facilities = len(state.Facilities)
	report.Locations = len(state.Locations)
	report.Overlays = len(state.Overlays)
	report.Activity = len(state.Activity)
	report.Message = "Dry run passed. No data was changed."
	return state, report, nil
}

func (s *Store) RestoreEventLog(data []byte, expectedSHA string, now time.Time) (RestoreReport, error) {
	state, report, err := ValidateRestore(data)
	if err != nil {
		return RestoreReport{}, err
	}
	if expectedSHA == "" || !strings.EqualFold(expectedSHA, report.SHA256) {
		return RestoreReport{}, validationError{"the selected file changed after validation; run the dry run again"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	directory := filepath.Join(s.dataDir, "snapshots")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return RestoreReport{}, fmt.Errorf("create pre-restore backup directory: %w", err)
	}
	preRestore := filepath.Join(directory, "pre-restore-"+now.UTC().Format("20060102T150405.000000000Z")+".ndjson")
	current, err := os.ReadFile(s.eventPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RestoreReport{}, fmt.Errorf("read current event log: %w", err)
	}
	if err := writeSyncedFile(preRestore, current); err != nil {
		return RestoreReport{}, fmt.Errorf("create mandatory pre-restore backup: %w", err)
	}
	temporary, err := os.CreateTemp(s.dataDir, ".restore-*.ndjson")
	if err != nil {
		return RestoreReport{}, fmt.Errorf("prepare restored event log: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return RestoreReport{}, err
	}
	if _, err = temporary.Write(data); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return RestoreReport{}, fmt.Errorf("write restored event log: %w", err)
	}
	oldPath := s.eventPath + ".replacing"
	_ = os.Remove(oldPath)
	if err := os.Rename(s.eventPath, oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return RestoreReport{}, fmt.Errorf("stage current event log: %w", err)
	}
	if err := os.Rename(temporaryPath, s.eventPath); err != nil {
		_ = os.Rename(oldPath, s.eventPath)
		return RestoreReport{}, fmt.Errorf("activate restored event log: %w", err)
	}
	_ = os.Remove(oldPath)
	if err := syncDirectory(s.dataDir); err != nil {
		return RestoreReport{}, err
	}
	state.Settings.APRS.Passcode = ""
	state.Settings.APRS.PasscodeConfigured = s.APRSPasscode() != ""
	s.state = state
	report.Message = "Restore completed. The prior event log was preserved as " + filepath.Base(preRestore) + "."
	event := Event{ID: newID("evt"), Kind: "system.restored", CreatedAt: now.UTC(), Change: []byte(`{}`)}
	for subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
	return report, nil
}

func writeSyncedFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = bytes.NewReader(data).WriteTo(file); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
