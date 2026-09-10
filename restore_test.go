package main

import (
	"bytes"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateAndApplyRestore(t *testing.T) {
	source, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.CreateIncident(IncidentInput{Title: "Restored incident", Type: "Other", Severity: "low", Status: "new", Address: "EOC"}); err != nil {
		t.Fatal(err)
	}
	var imported bytes.Buffer
	if err := source.EventLog(&imported); err != nil {
		t.Fatal(err)
	}
	_, report, err := ValidateRestore(imported.Bytes())
	if err != nil || report.Incidents != 1 || report.SHA256 == "" {
		t.Fatalf("validation report=%+v err=%v", report, err)
	}

	targetDir := t.TempDir()
	target, err := OpenStore(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.CreateResponder(ResponderInput{Name: "Current unit", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	var current bytes.Buffer
	if err := target.EventLog(&current); err != nil {
		t.Fatal(err)
	}
	if _, err := target.RestoreEventLog(imported.Bytes(), "wrong", time.Now()); err == nil {
		t.Fatal("restore accepted a mismatched validation hash")
	}
	applied, err := target.RestoreEventLog(imported.Bytes(), report.SHA256, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if applied.Incidents != 1 || len(target.Snapshot().Incidents) != 1 || len(target.Snapshot().Responders) != 0 {
		t.Fatalf("unexpected restored state: %+v", target.Snapshot())
	}
	backups, err := filepath.Glob(filepath.Join(targetDir, "snapshots", "pre-restore-*.ndjson"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("pre-restore backups=%v err=%v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, current.Bytes()) {
		t.Fatal("mandatory pre-restore backup does not match prior log")
	}
	replayed, err := OpenStore(targetDir)
	if err != nil || len(replayed.Snapshot().Incidents) != 1 {
		t.Fatalf("restored log did not replay: %v", err)
	}
}

func TestRestoreRejectsMalformedAndUnsafeLogs(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("not-json\n"),
		[]byte(`{"id":"evt-1","kind":"incident.created","created_at":"2026-01-01T00:00:00Z","change":{"incident":{"id":"../escape","number":1,"title":"Bad"}}}` + "\n"),
	} {
		if _, _, err := ValidateRestore(data); err == nil {
			t.Fatalf("unsafe restore was accepted: %s", data)
		}
	}
}

func TestRestoreAPIDryRunDoesNotMutate(t *testing.T) {
	dataDir := t.TempDir()
	store, _ := OpenStore(dataDir)
	if _, err := store.CreateResponder(ResponderInput{Name: "Existing", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	var logData bytes.Buffer
	if err := store.EventLog(&logData); err != nil {
		t.Fatal(err)
	}
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := restoreMultipart(t, logData.Bytes(), nil)
	request := httptest.NewRequest(http.MethodPost, "/api/restore/validate", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(store.Snapshot().Responders) != 1 {
		t.Fatalf("dry run status=%d state=%+v", response.Code, store.Snapshot())
	}
}

func restoreMultipart(t *testing.T, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "events.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}
