package main

import (
	"archive/zip"
	"bytes"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIncidentAttachmentUploadDownloadAndCompleteBackup(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	incident, err := store.CreateIncident(IncidentInput{Title: "Flood photo", Type: "Weather", Severity: "medium", Status: "new", Address: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "site.png")
	if err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0, 0, 0, 0, 0}
	_, _ = part.Write(png)
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/incidents/"+incident.ID+"/attachments", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	saved := store.Snapshot().Incidents[0]
	if len(saved.Attachments) != 1 || saved.Attachments[0].Name != "site.png" {
		t.Fatalf("attachment missing: %+v", saved.Attachments)
	}
	download := httptest.NewRecorder()
	server.Handler().ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/api/incidents/"+incident.ID+"/attachments/"+saved.Attachments[0].ID, nil))
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), png) {
		t.Fatalf("download mismatch status=%d", download.Code)
	}
	backup := httptest.NewRecorder()
	server.Handler().ServeHTTP(backup, httptest.NewRequest(http.MethodGet, "/api/backup/archive", nil))
	if backup.Code != http.StatusOK {
		t.Fatalf("backup status=%d", backup.Code)
	}
	archive, err := zip.NewReader(bytes.NewReader(backup.Body.Bytes()), int64(backup.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range archive.File {
		names = append(names, entry.Name)
	}
	joined := strings.Join(names, "|")
	if !strings.Contains(joined, "events.ndjson") || !strings.Contains(joined, saved.Attachments[0].StorageName) {
		t.Fatalf("complete backup missing content: %v", names)
	}
}

func TestIncidentAttachmentRejectsUnsupportedType(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	incident, _ := store.CreateIncident(IncidentInput{Title: "Test", Type: "Other", Severity: "low", Status: "new", Address: "Test"})
	server, _ := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "bad.txt")
	_, _ = part.Write([]byte("not an allowed attachment"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/incidents/"+incident.ID+"/attachments", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unsupported upload status=%d", recorder.Code)
	}
}
