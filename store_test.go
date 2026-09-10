package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStoreKeepsAPRSPasscodeOutsideEventLog(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveAPRSPasscode("12345"); err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.AreaEnabled = true
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !store.Snapshot().Settings.APRS.PasscodeConfigured {
		t.Fatal("saved APRS passcode is not reported as configured")
	}
	if store.APRSPasscode() != "12345" {
		t.Fatal("saved APRS passcode was not recovered")
	}
	info, err := os.Stat(filepath.Join(dataDir, "aprs-passcode"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows uses inherited directory ACLs, not POSIX permission bits.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("APRS passcode permissions = %o, want 600", info.Mode().Perm())
	}
	var backup bytes.Buffer
	if err = store.EventLog(&backup); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(backup.String(), "12345") || strings.Contains(backup.String(), `"passcode":`) {
		t.Fatal("APRS passcode leaked into the event log")
	}
	replayed, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.APRSPasscode() != "12345" || !replayed.Snapshot().Settings.APRS.PasscodeConfigured {
		t.Fatal("APRS passcode configuration was not recovered after restart")
	}
	if err = store.SaveAPRSPasscode("-1"); err == nil {
		t.Fatal("receive-only passcode -1 was accepted")
	}
}

func TestIncidentStaleAlertSettingDefaultsAndValidates(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Settings.IncidentStaleMinutes; got != 10 {
		t.Fatalf("incident stale alert default = %d, want 10", got)
	}

	settings := store.Snapshot().Settings
	settings.IncidentStaleMinutes = 30
	saved, err := store.UpdateSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	if saved.IncidentStaleMinutes != 30 {
		t.Fatalf("saved incident stale alert = %d, want 30", saved.IncidentStaleMinutes)
	}

	settings.IncidentStaleMinutes = 1441
	if _, err = store.UpdateSettings(settings); err == nil {
		t.Fatal("incident stale alert above 1,440 minutes was accepted")
	}
}

func TestStoreLifecycleAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	lat, lon := 35.9, -86.7
	responder, err := store.CreateResponder(ResponderInput{
		Name:         "Medic One",
		Callsign:     "M1",
		Type:         "Medical",
		Status:       "available",
		Capabilities: []string{"ALS", "Transport"},
		Latitude:     &lat,
		Longitude:    &lon,
	})
	if err != nil {
		t.Fatalf("CreateResponder: %v", err)
	}
	facility, err := store.CreateFacility(FacilityInput{
		Name:      "Community Hospital",
		Type:      "Hospital",
		Status:    "open",
		Address:   "10 Main Street",
		Capacity:  20,
		Occupied:  4,
		Latitude:  &lat,
		Longitude: &lon,
	})
	if err != nil {
		t.Fatalf("CreateFacility: %v", err)
	}
	if facility.ID == "" {
		t.Fatal("facility ID was not assigned")
	}
	incident, err := store.CreateIncident(IncidentInput{
		Title:       "Medical assistance",
		Type:        "Medical",
		Severity:    "high",
		Status:      "new",
		Address:     "20 Main Street",
		Description: "Caller reports a fall.",
		Latitude:    &lat,
		Longitude:   &lon,
	})
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	if incident.Number != 1001 {
		t.Fatalf("incident number = %d, want 1001", incident.Number)
	}
	incident, err = store.AssignResponder(incident.ID, responder.ID)
	if err != nil {
		t.Fatalf("AssignResponder: %v", err)
	}
	if incident.Status != "assigned" || len(incident.Assignments) != 1 {
		t.Fatalf("assignment did not update incident: %+v", incident)
	}
	if _, err = store.AddIncidentAction(incident.ID, "Unit notified by radio."); err != nil {
		t.Fatalf("AddIncidentAction: %v", err)
	}

	update := incidentInputFromIncident(incident)
	update.Status = "closed"
	incident, err = store.UpdateIncident(incident.ID, update)
	if err != nil {
		t.Fatalf("UpdateIncident: %v", err)
	}
	if incident.ClosedAt == nil {
		t.Fatal("closed incident has no close timestamp")
	}
	if len(incident.Assignments) != 0 {
		t.Fatalf("closed incident retained %d active assignments", len(incident.Assignments))
	}

	snapshot := store.Snapshot()
	if got := snapshot.Responders[0].Status; got != "available" {
		t.Fatalf("responder status after incident close = %q, want available", got)
	}
	if len(snapshot.Activity) < 6 {
		t.Fatalf("activity count = %d, want at least 6", len(snapshot.Activity))
	}

	var backup bytes.Buffer
	if err := store.EventLog(&backup); err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if !strings.Contains(backup.String(), `"incident.created"`) {
		t.Fatal("backup does not contain incident creation event")
	}

	replayed, err := OpenStore(dataDir)
	if err != nil {
		t.Fatalf("OpenStore replay: %v", err)
	}
	replayedState := replayed.Snapshot()
	if len(replayedState.Incidents) != 1 || len(replayedState.Responders) != 1 || len(replayedState.Facilities) != 1 {
		t.Fatalf("unexpected replayed state: %+v", replayedState)
	}
	if replayedState.Incidents[0].Status != "closed" || len(replayedState.Incidents[0].Actions) != 1 {
		t.Fatalf("incident replay lost lifecycle state: %+v", replayedState.Incidents[0])
	}
}

func TestStoreRejectsUnavailableResponder(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	responder, err := store.CreateResponder(ResponderInput{Name: "Unit 1", Status: "out_of_service"})
	if err != nil {
		t.Fatal(err)
	}
	incident, err := store.CreateIncident(IncidentInput{
		Title: "Check", Type: "Other", Severity: "low", Status: "new", Address: "Staging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AssignResponder(incident.ID, responder.ID); err == nil {
		t.Fatal("AssignResponder accepted an out-of-service responder")
	}
}

func TestStoreRetainsMultipleRespondersAfterReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.CreateResponder(ResponderInput{
		Name: "Medic 1", Callsign: "MEDIC-1", Status: "available",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateResponder(ResponderInput{
		Name: "Search 2", Callsign: "SEARCH-2", Status: "available",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("responders received the same ID %q", first.ID)
	}

	state := store.Snapshot()
	if len(state.Responders) != 2 {
		t.Fatalf("responder count = %d, want 2: %+v", len(state.Responders), state.Responders)
	}
	if state.Responders[0].Name != "Medic 1" || state.Responders[1].Name != "Search 2" {
		t.Fatalf("one responder overwrote another: %+v", state.Responders)
	}

	replayed, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	replayedState := replayed.Snapshot()
	if len(replayedState.Responders) != 2 {
		t.Fatalf("replayed responder count = %d, want 2: %+v", len(replayedState.Responders), replayedState.Responders)
	}
}

func TestStoreRetainsMultipleLocationsAndOverlayAfterReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	lat1, lon1 := 35.91, -86.71
	lat2, lon2 := 35.92, -86.72
	first, err := store.CreateLocation(LocationInput{
		Name: "North Staging", Type: "Staging", Address: "North trailhead", Latitude: &lat1, Longitude: &lon1,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateLocation(LocationInput{
		Name: "South Access", Type: "Access", Address: "South gate", Latitude: &lat2, Longitude: &lon2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("locations received the same ID %q", first.ID)
	}
	overlay, err := store.CreateOverlay(MapOverlayInput{
		Name: "Evacuation Route", FileName: "route.kml", Color: "#FF6600", Visible: true,
		Features: []OverlayFeature{{
			Name: "Route A", GeometryType: "line", Paths: [][]MapCoordinate{{
				{Latitude: lat1, Longitude: lon1},
				{Latitude: lat2, Longitude: lon2},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateOverlay(overlay.ID, MapOverlayUpdateInput{Name: overlay.Name, Color: "#00AAFF", Visible: false}); err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.CenterAddress = "100 Emergency Operations Way"
	settings.CenterLat = lat1
	settings.CenterLon = lon1
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	state := store.Snapshot()
	if len(state.Locations) != 2 || len(state.Overlays) != 1 {
		t.Fatalf("records overwrote each other: locations=%+v overlays=%+v", state.Locations, state.Overlays)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	replayedState := replayed.Snapshot()
	if len(replayedState.Locations) != 2 || len(replayedState.Overlays) != 1 {
		t.Fatalf("replay lost map records: %+v", replayedState)
	}
	if replayedState.Overlays[0].Visible || replayedState.Overlays[0].Color != "#00AAFF" {
		t.Fatalf("overlay metadata update was not replayed: %+v", replayedState.Overlays[0])
	}
	if replayedState.Settings.CenterAddress != "100 Emergency Operations Way" {
		t.Fatalf("map center address was not replayed: %+v", replayedState.Settings)
	}
}

func TestStoreValidation(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lat := 95.0
	if _, err := store.CreateIncident(IncidentInput{
		Title: "Invalid", Type: "Other", Severity: "low", Status: "new", Address: "Somewhere", Latitude: &lat,
	}); err == nil {
		t.Fatal("CreateIncident accepted an incomplete, invalid coordinate pair")
	}
	if _, err := store.CreateFacility(FacilityInput{Name: "Shelter", Status: "open", Capacity: 10, Occupied: 11}); err == nil {
		t.Fatal("CreateFacility accepted occupied > capacity")
	}
	if _, err := store.CreateLocation(LocationInput{Name: "Bad", Address: "Somewhere", Latitude: &lat}); err == nil {
		t.Fatal("CreateLocation accepted an incomplete coordinate pair")
	}
}

func TestStoreRecordsAndReplaysAPRSPosition(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	responder, err := store.CreateResponder(ResponderInput{
		Name: "Tracker", Callsign: "N0CALL-7", Status: "available", APRSEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	receivedAt := time.Now().UTC().Truncate(time.Millisecond)
	speed := 22.0
	updated, matched, err := store.RecordAPRSPosition(APRSPosition{
		Callsign: "N0CALL-7", Latitude: 35.1, Longitude: -86.7,
		SpeedKnots: &speed, Symbol: "/>", Comment: "Mobile", ReceivedAt: receivedAt,
	}, "aprs_is")
	if err != nil {
		t.Fatal(err)
	}
	if !matched || updated.ID != responder.ID || updated.PositionSource != "aprs_is" {
		t.Fatalf("position did not match responder: %+v matched=%t", updated, matched)
	}
	snapshot := store.Snapshot()
	if len(snapshot.Tracks) != 1 || snapshot.Responders[0].PositionUpdatedAt == nil {
		t.Fatalf("APRS position was not reflected in state: %+v", snapshot)
	}

	replayed, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	replayedState := replayed.Snapshot()
	if len(replayedState.Tracks) != 1 || replayedState.Responders[0].Latitude == nil ||
		*replayedState.Responders[0].Latitude != 35.1 {
		t.Fatalf("APRS position was not replayed: %+v", replayedState)
	}
}

func TestStoreChangingTrackedCallsignClearsOldAPRSPositionAndTrail(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	responder, err := store.CreateResponder(ResponderInput{
		Name: "Tracker", Callsign: "N0CALL-7", Status: "available", APRSEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	receivedAt := time.Now().UTC().Truncate(time.Millisecond)
	if _, matched, err := store.RecordAPRSPosition(APRSPosition{
		Callsign: "N0CALL-7", Latitude: 35.1, Longitude: -86.7,
		Symbol: "/>", ReceivedAt: receivedAt,
	}, "aprs_is"); err != nil || !matched {
		t.Fatalf("RecordAPRSPosition matched=%t err=%v", matched, err)
	}
	current := store.Snapshot().Responders[0]
	updated, err := store.UpdateResponder(responder.ID, ResponderInput{
		Name: "Tracker", Callsign: "N0CALL-9", Status: "available",
		Latitude: current.Latitude, Longitude: current.Longitude, APRSEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Latitude != nil || updated.Longitude != nil || updated.PositionUpdatedAt != nil ||
		updated.PositionSource != "" {
		t.Fatalf("old APRS position survived callsign change: %+v", updated)
	}
	if tracks := store.Snapshot().Tracks; len(tracks) != 0 {
		t.Fatalf("old APRS trail survived callsign change: %+v", tracks)
	}

	replayed, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	state := replayed.Snapshot()
	if len(state.Tracks) != 0 || state.Responders[0].Latitude != nil ||
		state.Responders[0].PositionUpdatedAt != nil {
		t.Fatalf("replay restored cleared APRS data: %+v", state)
	}
}

func incidentInputFromIncident(incident Incident) IncidentInput {
	return IncidentInput{
		Title:        incident.Title,
		Type:         incident.Type,
		Severity:     incident.Severity,
		Status:       incident.Status,
		Address:      incident.Address,
		City:         incident.City,
		Region:       incident.Region,
		PostalCode:   incident.PostalCode,
		Latitude:     incident.Latitude,
		Longitude:    incident.Longitude,
		ContactName:  incident.ContactName,
		ContactPhone: incident.ContactPhone,
		Description:  incident.Description,
	}
}
