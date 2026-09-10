package main

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestOpenGTSLocationAPIAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, _ := OpenStore(dataDir)
	responder, _ := store.CreateResponder(ResponderInput{Name: "Vehicle Tracker", Status: "available"})
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	values := url.Values{"lat": {"35.9765"}, "lon": {"-86.6628"}, "date": {now.Format("20060102")}, "time": {now.Format("150405")}, "speed": {"18.52"}, "head": {"180"}, "alt": {"100"}}
	recorder := jsonRequest(t, server.Handler(), http.MethodGet, "/api/tracking/opengts/"+responder.ID+"?"+values.Encode(), nil)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "OK\n" {
		t.Fatalf("OpenGTS API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	updated := store.Snapshot().Responders[0]
	if updated.PositionSource != "opengts" || updated.SpeedKnots == nil || *updated.SpeedKnots != 10 || len(store.Snapshot().Tracks) != 1 {
		t.Fatalf("unexpected OpenGTS position: %+v", updated)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || replayed.Snapshot().Responders[0].PositionSource != "opengts" {
		t.Fatalf("OpenGTS replay failed: %+v %v", replayed.Snapshot().Responders, err)
	}
}

func TestOpenGTSValidation(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	responder, _ := store.CreateResponder(ResponderInput{Name: "Tracker", Status: "available"})
	server, _ := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	for _, query := range []string{"?lon=-86", "?lat=91&lon=0", "?lat=35&lon=-86&head=400", "?lat=35&lon=-86&date=bad&time=bad"} {
		recorder := jsonRequest(t, server.Handler(), http.MethodGet, "/api/tracking/opengts/"+responder.ID+query, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected rejection for %s, got %d", query, recorder.Code)
		}
	}
}
