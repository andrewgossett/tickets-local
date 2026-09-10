package main

import (
	"io"
	"log"
	"net/http"
	"testing"
	"time"
)

func TestOwnTracksLocationAPIAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, _ := OpenStore(dataDir)
	responder, _ := store.CreateResponder(ResponderInput{Name: "Field Phone", Status: "available"})
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Second).Unix()
	payload := map[string]any{"_type": "location", "lat": 35.9765, "lon": -86.6628, "tst": now, "acc": 8, "vel": 18.52, "cog": 90, "alt": 100}
	recorder := jsonRequest(t, server.Handler(), http.MethodPost, "/api/tracking/owntracks/"+responder.ID, payload)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "[]\n" {
		t.Fatalf("OwnTracks API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	updated := store.Snapshot().Responders[0]
	if updated.PositionSource != "owntracks" || updated.SpeedKnots == nil || *updated.SpeedKnots != 10 || len(store.Snapshot().Tracks) != 1 {
		t.Fatalf("unexpected OwnTracks position: %+v tracks=%+v", updated, store.Snapshot().Tracks)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || replayed.Snapshot().Responders[0].PositionSource != "owntracks" || len(replayed.Snapshot().Tracks) != 1 {
		t.Fatalf("OwnTracks replay failed: %+v %v", replayed.Snapshot(), err)
	}
}

func TestOwnTracksRejectsInvalidAndOldPayloads(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	responder, _ := store.CreateResponder(ResponderInput{Name: "Field Phone", Status: "available"})
	server, _ := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	recorder := jsonRequest(t, server.Handler(), http.MethodPost, "/api/tracking/owntracks/"+responder.ID, map[string]any{"_type": "transition", "lat": 35, "lon": -86})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected type rejection, got %d", recorder.Code)
	}
	recorder = jsonRequest(t, server.Handler(), http.MethodPost, "/api/tracking/owntracks/"+responder.ID, map[string]any{"_type": "location", "lat": 91, "lon": 0})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected coordinate rejection, got %d", recorder.Code)
	}
}
