package main

import (
	"io"
	"log"
	"net/http"
	"testing"
)

func TestResponderDevicePositionAPIAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	responder, _ := store.CreateResponder(ResponderInput{Name: "Field Team", Status: "available"})
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	accuracy := 12.5
	recorder := jsonRequest(t, server.Handler(), http.MethodPost, "/api/responders/"+responder.ID+"/position", ResponderPositionInput{Latitude: 35.9765, Longitude: -86.6628, AccuracyMeters: &accuracy, ExpectedUpdatedAt: &responder.UpdatedAt})
	if recorder.Code != http.StatusOK {
		t.Fatalf("position API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	updated := store.Snapshot().Responders[0]
	if updated.Latitude == nil || *updated.Latitude != 35.9765 || updated.PositionSource != "device" || updated.PositionUpdatedAt == nil {
		t.Fatalf("unexpected position: %+v", updated)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || replayed.Snapshot().Responders[0].PositionSource != "device" {
		t.Fatalf("position replay failed: %+v %v", replayed.Snapshot().Responders, err)
	}
}

func TestResponderDevicePositionValidationAndConflict(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	responder, _ := store.CreateResponder(ResponderInput{Name: "Field Team", Status: "available"})
	if _, err := store.UpdateResponderDevicePosition(responder.ID, ResponderPositionInput{Latitude: 91, Longitude: 0}); err == nil {
		t.Fatal("expected coordinate validation")
	}
	stale := responder.UpdatedAt.Add(-1)
	if _, err := store.UpdateResponderDevicePosition(responder.ID, ResponderPositionInput{Latitude: 35, Longitude: -86, ExpectedUpdatedAt: &stale}); err == nil {
		t.Fatal("expected stale update conflict")
	}
}
