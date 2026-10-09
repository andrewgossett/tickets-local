package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWeatherServiceFetchesBoundsAndCachesNWSAlerts(t *testing.T) {
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.URL.Query().Get("point"); got != "35.9682,-86.5190" {
			t.Errorf("point query = %q", got)
		}
		if got := r.URL.Query().Get("status"); got != "actual" {
			t.Errorf("status query = %q", got)
		}
		w.Header().Set("Content-Type", "application/geo+json")
		_, _ = io.WriteString(w, `{
			"features":[{
				"id":"urn:oid:test-warning",
				"geometry":{"type":"Polygon","coordinates":[[[-86.6,35.9],[-86.4,35.9],[-86.4,36.1],[-86.6,35.9]]]},
				"properties":{"event":"Tornado Warning","headline":"Tornado Warning issued","severity":"Extreme","urgency":"Immediate","certainty":"Observed","description":"Take shelter.","instruction":"Move indoors.","areaDesc":"Williamson County","effective":"2026-09-06T12:00:00Z","expires":"2026-09-06T12:30:00Z"}
			}]
		}`)
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", provider.URL)

	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.CenterLat = 35.9682
	settings.CenterLon = -86.5190
	settings.Weather.Enabled = true
	settings.Weather.RefreshMinutes = 5
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	service := NewWeatherService(store, provider.Client(), log.New(io.Discard, "", 0))

	status := service.Status(context.Background())
	if status.State != "current" || status.Stale || len(status.Alerts) != 1 {
		t.Fatalf("unexpected weather status: %+v", status)
	}
	alert := status.Alerts[0]
	if alert.Event != "Tornado Warning" || alert.Severity != "extreme" || len(alert.Paths) != 1 || len(alert.Paths[0]) != 4 {
		t.Fatalf("unexpected normalized alert: %+v", alert)
	}
	if second := service.Status(context.Background()); second.State != "current" || requests.Load() != 1 {
		t.Fatalf("weather cache was not reused: status=%+v requests=%d", second, requests.Load())
	}
	if _, err := os.Stat(filepath.Join(dataDir, "weather-cache.json")); err != nil {
		t.Fatalf("weather cache was not persisted: %v", err)
	}
	backup := new(strings.Builder)
	if err := store.EventLog(backup); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(backup.String(), "Tornado Warning issued") {
		t.Fatal("transient weather data entered the operational event log")
	}
}

func TestWeatherServiceUsesAffectedZoneGeometryWhenAlertHasNoPolygon(t *testing.T) {
	var zoneRequests atomic.Int32
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alerts":
			_, _ = io.WriteString(w, `{"features":[{
				"id":"urn:oid:flood-watch",
				"geometry":null,
				"properties":{
					"event":"Flood Watch",
					"severity":"Severe",
					"areaDesc":"Williamson County",
					"affectedZones":["`+provider.URL+`/zones/forecast/TNZ075","`+provider.URL+`/zones/forecast/TNZ075","https://untrusted.example/zones/forecast/EVIL"]
				}
			}]}`)
		case "/zones/forecast/TNZ075":
			zoneRequests.Add(1)
			_, _ = io.WriteString(w, `{"geometry":{"type":"MultiPolygon","coordinates":[[[[-86.9,35.7],[-86.5,35.7],[-86.5,36.1],[-86.9,35.7]]]]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", provider.URL+"/alerts")
	t.Setenv("TICKETS_LOCAL_NWS_API_URL", provider.URL)

	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	status := NewWeatherService(store, provider.Client(), log.New(io.Discard, "", 0)).Status(context.Background())
	if status.State != "current" || len(status.Alerts) != 1 {
		t.Fatalf("unexpected weather status: %+v", status)
	}
	alert := status.Alerts[0]
	if alert.Event != "Flood Watch" || len(alert.Paths) != 1 || len(alert.Paths[0]) != 4 {
		t.Fatalf("affected-zone geometry was not applied: %+v", alert)
	}
	if zoneRequests.Load() != 1 {
		t.Fatalf("affected zone fetched %d times; want one bounded deduplicated request", zoneRequests.Load())
	}
}

func TestWeatherServiceFetchesAndCachesVisibleMapAlerts(t *testing.T) {
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("point") == "" || r.URL.Query().Get("status") != "actual" {
			t.Errorf("visible-map request query = %q", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"features":[{
			"id":"urn:oid:hurricane-warning",
			"geometry":{"type":"Polygon","coordinates":[[[-82.8,26.0],[-80.0,26.0],[-80.0,29.0],[-82.8,26.0]]]},
			"properties":{"event":"Hurricane Warning","severity":"Extreme","areaDesc":"Florida coast"}
		}]}`)
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", provider.URL)
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	settings.Weather.RefreshMinutes = 5
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	service := NewWeatherService(store, provider.Client(), log.New(io.Discard, "", 0))
	status, err := service.AlertsForBounds(context.Background(), 24, -87, 31, -79)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "current" || len(status.Alerts) != 1 || status.Alerts[0].Event != "Hurricane Warning" {
		t.Fatalf("unexpected visible-map alerts: %+v", status)
	}
	if requests.Load() != maxViewportSamples {
		t.Fatalf("visible map made %d requests; want %d bounded samples", requests.Load(), maxViewportSamples)
	}
	if _, err := service.AlertsForBounds(context.Background(), 24, -87, 31, -79); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != maxViewportSamples {
		t.Fatalf("visible-map alert cache was not reused: %d requests", requests.Load())
	}
	if _, err := service.AlertsForBounds(context.Background(), 31, -87, 24, -79); err == nil {
		t.Fatal("inverted visible-map bounds were accepted")
	}
}

func TestWeatherServiceAddsCenterStateAlertsToVisibleMap(t *testing.T) {
	var areaRequests atomic.Int32
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/points/"):
			_, _ = io.WriteString(w, `{"properties":{"relativeLocation":{"properties":{"state":"FL"}}}}`)
		case r.URL.Path == "/alerts" && r.URL.Query().Get("area") == "FL":
			areaRequests.Add(1)
			_, _ = io.WriteString(w, `{"features":[{"id":"state-hurricane","geometry":{"type":"Polygon","coordinates":[[[-87.0,29.0],[-85.0,29.0],[-85.0,30.5],[-87.0,29.0]]]},"properties":{"event":"Hurricane Warning","severity":"Extreme","areaDesc":"Florida Gulf Coast"}}]}`)
		case r.URL.Path == "/alerts" && r.URL.Query().Get("point") != "":
			_, _ = io.WriteString(w, `{"features":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", provider.URL+"/alerts")
	t.Setenv("TICKETS_LOCAL_NWS_API_URL", provider.URL)
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	status, err := NewWeatherService(store, provider.Client(), log.New(io.Discard, "", 0)).AlertsForBounds(context.Background(), 24, -87, 31, -79)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Alerts) != 1 || status.Alerts[0].Event != "Hurricane Warning" || areaRequests.Load() != 1 {
		t.Fatalf("center-state hurricane alert was not included once: status=%+v area requests=%d", status, areaRequests.Load())
	}
}

func TestLimitWeatherAlertCoordinatesPreservesSeverityOrder(t *testing.T) {
	path := func(count int) []MapCoordinate {
		result := make([]MapCoordinate, count)
		for index := range result {
			result[index] = MapCoordinate{Latitude: float64(index), Longitude: float64(index)}
		}
		return result
	}
	alerts := []WeatherAlert{
		{ID: "extreme", Severity: "extreme", Paths: [][]MapCoordinate{path(6)}},
		{ID: "minor", Severity: "minor", Paths: [][]MapCoordinate{path(6)}},
	}
	limited := limitWeatherAlertCoordinates(alerts, 9)
	if len(limited[0].Paths) != 1 || len(limited[0].Paths[0]) != 6 || len(limited[1].Paths) != 1 || len(limited[1].Paths[0]) != 3 {
		t.Fatalf("coordinate cap did not preserve higher-severity geometry first: %+v", limited)
	}
}

func TestWeatherServiceUsesPersistedCacheWhenProviderFails(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Now().UTC().Add(-time.Hour)
	cached := WeatherStatus{
		State: "current", Message: "old", UpdatedAt: &now, Source: "National Weather Service",
		CenterLat: 35.9682, CenterLon: -86.5190,
		Alerts: []WeatherAlert{{ID: "cached", Event: "Flood Warning", Severity: "severe"}},
	}
	data, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "weather-cache.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.CenterLat = cached.CenterLat
	settings.CenterLon = cached.CenterLon
	settings.Weather.Enabled = true
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", "http://127.0.0.1:1/unavailable")
	service := NewWeatherService(store, &http.Client{Timeout: 100 * time.Millisecond}, log.New(io.Discard, "", 0))
	status := service.Status(context.Background())
	if status.State != "stale" || !status.Stale || len(status.Alerts) != 1 || status.Alerts[0].ID != "cached" {
		t.Fatalf("persisted weather cache was not retained: %+v", status)
	}
}

func TestWeatherServiceDisabledDoesNotCallProvider(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("disabled weather service called provider")
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", provider.URL)
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewWeatherService(store, provider.Client(), log.New(io.Discard, "", 0))
	status := service.Status(context.Background())
	if status.State != "disabled" || status.Alerts == nil {
		t.Fatalf("unexpected disabled weather status: %+v", status)
	}
}

func TestWeatherAPIReportsHostOwnedAlerts(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"features":[{"id":"api-alert","geometry":null,"properties":{"event":"Flood Advisory","severity":"Minor","urgency":"Expected","certainty":"Likely","areaDesc":"Test Area"}}]}`)
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", provider.URL)
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()
	status := requestJSON[WeatherStatus](t, server.URL+"/api/weather", http.MethodGet, nil, http.StatusOK)
	if status.State != "current" || len(status.Alerts) != 1 || status.Alerts[0].ID != "api-alert" {
		t.Fatalf("unexpected weather API response: %+v", status)
	}
	viewport := requestJSON[WeatherAlertViewport](t, server.URL+"/api/weather/alerts?south=24&west=-87&north=31&east=-79", http.MethodGet, nil, http.StatusOK)
	if viewport.State != "current" || len(viewport.Alerts) != 1 || viewport.Alerts[0].ID != "api-alert" {
		t.Fatalf("unexpected visible-map weather API response: %+v", viewport)
	}
	requestJSON[map[string]string](t, server.URL+"/api/weather/alerts?south=31&west=-87&north=24&east=-79", http.MethodGet, nil, http.StatusBadRequest)
}

func TestWeatherSettingsValidateAndUseConfiguredLocalSource(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"features":[]}`)
	}))
	defer local.Close()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	settings.Weather.AlertsURL = local.URL
	settings.Weather.RadarURL = local.URL + "/radar"
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	service := NewWeatherService(store, local.Client(), log.New(io.Discard, "", 0))
	status := service.Status(context.Background())
	if status.State != "current" || !strings.Contains(status.Source, "127.0.0.1") {
		t.Fatalf("configured local weather source was not used: %+v", status)
	}
	settings.Weather.AlertsURL = "http://user:secret@mesh.local/alerts"
	if _, err := store.UpdateSettings(settings); err == nil {
		t.Fatal("weather source URL containing credentials was accepted")
	}
}

func TestWeatherServiceAddsNearestNWSObservation(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/alerts"):
			_, _ = io.WriteString(w, `{"features":[]}`)
		case strings.HasPrefix(r.URL.Path, "/points/"):
			_, _ = io.WriteString(w, `{"properties":{"observationStations":"`+provider.URL+`/stations"}}`)
		case r.URL.Path == "/stations":
			_, _ = io.WriteString(w, `{"features":[{"id":"`+provider.URL+`/stations/KTEST"}]}`)
		case r.URL.Path == "/stations/KTEST/observations/latest":
			_, _ = io.WriteString(w, `{"properties":{"textDescription":"Partly Cloudy","timestamp":"2026-09-06T15:00:00Z","temperature":{"value":20},"heatIndex":{"value":null},"windChill":{"value":null},"relativeHumidity":{"value":55},"windSpeed":{"value":16.0934},"windDirection":{"value":180},"barometricPressure":{"value":101320},"visibility":{"value":16093.4}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_NWS_ALERTS_URL", provider.URL+"/alerts")
	t.Setenv("TICKETS_LOCAL_NWS_API_URL", provider.URL)
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	status := NewWeatherService(store, provider.Client(), log.New(io.Discard, "", 0)).Status(context.Background())
	if status.Current == nil || status.Current.TemperatureF == nil || *status.Current.TemperatureF != 68 || status.Current.Station != "KTEST" || status.Current.WindSpeedMPH == nil {
		t.Fatalf("unexpected current conditions: %+v", status.Current)
	}
}
