package main

import (
	"io"
	"log"
	"net/http"
	"testing"
)

func TestOperationalMessageLifecycleReplayAndAPI(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	incident, _ := store.CreateIncident(IncidentInput{Title: "Field operation", Type: "Other", Severity: "medium", Address: "EOC"})
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	recorder := jsonRequest(t, server.Handler(), http.MethodPost, "/api/messages", OperationalMessageInput{Channel: "operations", IncidentID: incident.ID, Author: "N0CALL", Body: "Team arrived at staging."})
	if recorder.Code != http.StatusCreated || len(store.Snapshot().Messages) != 1 {
		t.Fatalf("message API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	message := store.Snapshot().Messages[0]
	if message.Channel != "operations" || message.IncidentID != incident.ID || message.Body != "Team arrived at staging." {
		t.Fatalf("unexpected message: %+v", message)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || len(replayed.Snapshot().Messages) != 1 || replayed.Snapshot().Messages[0].ID != message.ID {
		t.Fatalf("message replay failed: %+v %v", replayed.Snapshot().Messages, err)
	}
}

func TestOperationalMessageValidation(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	if _, err := store.CreateOperationalMessage(OperationalMessageInput{Channel: "external", Body: "No"}); err == nil {
		t.Fatal("expected channel validation")
	}
	if _, err := store.CreateOperationalMessage(OperationalMessageInput{Channel: "general", Body: "  "}); err == nil {
		t.Fatal("expected body validation")
	}
	if _, err := store.CreateOperationalMessage(OperationalMessageInput{Channel: "command", IncidentID: "missing", Body: "Update"}); err == nil {
		t.Fatal("expected incident validation")
	}
}
