package main

import (
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAPRSWatchCallsignWithoutResponderAssignment(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.WatchCallsigns = []string{"ki4hdu-8", "KI4HDU-8"}
	settings, err = store.UpdateSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.APRS.WatchCallsigns) != 1 || settings.APRS.WatchCallsigns[0] != "KI4HDU-8" {
		t.Fatalf("watch callsigns were not normalized: %#v", settings.APRS.WatchCallsigns)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	config := manager.config()
	if config.filter != "b/KI4HDU*" || len(config.callsign) != 0 {
		t.Fatalf("unexpected independent watch config: filter=%q callsigns=%v", config.filter, config.callsign)
	}
	position := APRSPosition{Callsign: "KI4HDU-8", Latitude: 35.95, Longitude: -83.56, ReceivedAt: time.Now().UTC(), Raw: "KI4HDU-8>APRS:!3557.00N/08333.60W>Watch"}
	manager.recordActivity(position, "aprs_is")
	activity := manager.Activity()
	if len(activity) != 1 || activity[0].Callsign != "KI4HDU-8" {
		t.Fatalf("watched station was not displayed independently: %+v", activity)
	}

	settings.APRS.WatchCallsigns = []string{"not a callsign"}
	if _, err := store.UpdateSettings(settings); err == nil {
		t.Fatal("invalid APRS watch callsign was accepted")
	}
}

func TestResponderMapPositionDeleteAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	responder, err := store.CreateResponder(ResponderInput{Name: "Field Team", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateResponderDevicePosition(responder.ID, ResponderPositionInput{
		Latitude: 35.95, Longitude: -83.56, Source: "map", ExpectedUpdatedAt: &responder.UpdatedAt,
	})
	if err != nil || updated.PositionSource != "map" {
		t.Fatalf("map position failed: %+v %v", updated, err)
	}
	if err = store.DeleteResponder(responder.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.Snapshot().Responders) != 0 || len(store.Snapshot().Tracks) != 0 {
		t.Fatalf("responder or tracks remained after delete: %+v", store.Snapshot())
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || len(replayed.Snapshot().Responders) != 0 {
		t.Fatalf("responder deletion did not replay: %+v %v", replayed.Snapshot().Responders, err)
	}
}

func TestResponderDeleteAPIAndUIControls(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	responder, _ := store.CreateResponder(ResponderInput{Name: "Delete Me", Status: "available"})
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	recorder := jsonRequest(t, server.Handler(), http.MethodDelete, "/api/responders/"+responder.ID, nil)
	if recorder.Code != http.StatusNoContent || len(store.Snapshot().Responders) != 0 {
		t.Fatalf("delete API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	index, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := webAssets.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"watch_callsigns", "position-responder-on-map", "delete-responder"} {
		if !strings.Contains(string(index), required) {
			t.Fatalf("index is missing %q", required)
		}
	}
	for _, required := range []string{"chooseResponderPositionOnMap", "makeResponderDraggable", "deleteResponder"} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("script is missing %q", required)
		}
	}
}
