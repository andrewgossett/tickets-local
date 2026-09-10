package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConnectionDiagnosticsShowsDisabledServicesWithoutFailures(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	recorder := jsonRequest(t, server.Handler(), http.MethodGet, "/api/connections", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("connection diagnostics status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var diagnostics ConnectionDiagnostics
	decodeRecorderJSON(t, recorder, &diagnostics)
	if diagnostics.Overall != "connected" || len(diagnostics.Items) < 7 {
		t.Fatalf("unexpected default diagnostics: %+v", diagnostics)
	}
	states := map[string]string{}
	for _, item := range diagnostics.Items {
		states[item.ID] = item.State
	}
	if states["local-app"] != "connected" || states["nws"] != "disabled" || states["aprs-is"] != "disabled" || states["water"] != "disabled" {
		t.Fatalf("unexpected default connection states: %+v", states)
	}
}

func TestConnectionDiagnosticsTestsNWSAndRadar(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alerts":
			w.Header().Set("Content-Type", "application/geo+json")
			_, _ = io.WriteString(w, `{"features":[]}`)
		case "/radar":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("bounded-test-png"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	settings.Weather.RadarEnabled = true
	settings.Weather.RefreshMinutes = 5
	settings.Weather.AlertsURL = provider.URL + "/alerts"
	settings.Weather.RadarURL = provider.URL + "/radar"
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server.client = provider.Client()
	server.weather.client = provider.Client()
	recorder := jsonRequest(t, server.Handler(), http.MethodGet, "/api/connections", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("connection diagnostics status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var diagnostics ConnectionDiagnostics
	decodeRecorderJSON(t, recorder, &diagnostics)
	states := map[string]string{}
	for _, item := range diagnostics.Items {
		states[item.ID] = item.State
	}
	if states["nws"] != "connected" || states["nws-radar"] != "connected" {
		t.Fatalf("NWS connection checks did not succeed: %+v", diagnostics.Items)
	}
}

func TestConnectionDiagnosticsReportsAPRSInternetAndLocalRF(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	manager.updateStatus(func(status *APRSStatus) {
		status.State = "connected"
		status.Message = "Verified APRS-IS feed is live"
	})
	manager.updateLocalStatus(func(status *APRSLocalStatus) {
		status.State = "connected"
		status.Message = "Dire Wolf KISS receiver is live"
	})
	server := &apiServer{aprs: manager}
	checks := server.aprsConnectionChecks(APRSSettings{Enabled: true, Mode: "hybrid"})
	if len(checks) != 2 || checks[0].State != "connected" || checks[1].State != "connected" {
		t.Fatalf("unexpected APRS connection checks: %+v", checks)
	}
}

func TestIntegrationFeedCheckDoesNotCallEmptyRepeaterDataConnected(t *testing.T) {
	now := time.Now().UTC()
	check := integrationFeedCheck("amateur-repeaters", "Amateur repeaters", "OpenStreetMap", 0, IntegrationStatus{State: "current", UpdatedAt: &now}, true)
	if check.State != "stale" || check.Detail != "Provider responded, but no map data was returned" {
		t.Fatalf("unexpected empty repeater check: %+v", check)
	}
	storm := integrationFeedCheck("storm-reports", "Storm reports", "https://example.test", 0, IntegrationStatus{State: "current", UpdatedAt: &now}, false)
	if storm.State != "connected" {
		t.Fatalf("an empty event-style feed should still report provider connectivity: %+v", storm)
	}
}

func decodeRecorderJSON(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
