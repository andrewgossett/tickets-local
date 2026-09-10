package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIntegrationServiceLoadsBoundedOperationalFeeds(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/storm":
			_, _ = io.WriteString(w, `{"features":[{"id":"s1","geometry":{"type":"Point","coordinates":[-86.8,35.9]},"properties":{"event":"Hail","source":"NWS","observed_at":"2026-09-08T14:00:00Z"}}]}`)
		case "/roads":
			_, _ = io.WriteString(w, `{"features":[{"id":"r1","geometry":{"type":"Point","coordinates":[-86.7,35.8]},"properties":{"name":"Bridge closed","source":"County EOC","timestamp":"2026-09-08T14:01:00Z"}}]}`)
		case "/ham":
			_, _ = io.WriteString(w, `{"features":[{"id":"h1","geometry":{"type":"Point","coordinates":[-86.69,35.89]},"properties":{"callsign":"W4XYZ","frequency":"146.940","offset":"-0.600","tone":"123.0","mode":"FM","source":"Club export"}}]}`)
		case "/gmrs":
			_, _ = io.WriteString(w, `{"features":[{"id":"g1","geometry":{"type":"Point","coordinates":[-86.68,35.88]},"properties":{"name":"County GMRS","output_frequency":"462.675","ctcss":"141.3","access":"permission","source":"Local coordinator"}}]}`)
		case "/meshcore":
			_, _ = io.WriteString(w, `{"version":1,"nodes":[{"id":"mc1","name":"Hilltop","type":"repeater","latitude":35.87,"longitude":-86.67,"last_seen_at":"2026-09-08T14:02:30Z","snr":8.5,"battery_percent":92,"details":"Local gateway advert"}]}`)
		case "/node":
			_, _ = io.WriteString(w, `{"name":"mesh-east","status":"online","route_cost":2.4}`)
		case "/sensor":
			_, _ = io.WriteString(w, `{"version":1,"observations":[{"id":"wx1","name":"Shelter","observed_at":"2026-09-08T14:02:00Z","latitude":35.7,"longitude":-86.6,"temperature_f":81,"battery_percent":90,"status":"normal"}]}`)
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
	settings.Integrations = IntegrationSettings{Enabled: true, RefreshMinutes: 5, StormReportsURL: provider.URL + "/storm", InfrastructureURL: provider.URL + "/roads", AmateurRepeatersURL: provider.URL + "/ham", GMRSRepeatersURL: provider.URL + "/gmrs", MeshCoreURL: provider.URL + "/meshcore", AREDNNodeURLs: provider.URL + "/node", SensorURLs: provider.URL + "/sensor"}
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	status := NewIntegrationService(store, provider.Client(), log.New(io.Discard, "", 0)).Status(context.Background())
	if status.State != "current" || len(status.StormReports) != 1 || len(status.Infrastructure) != 1 || len(status.AmateurRepeaters) != 1 || len(status.GMRSRepeaters) != 1 || len(status.MeshCoreNodes) != 1 || len(status.AREDNNodes) != 1 || !status.AREDNNodes[0].Reachable || status.AREDNNodes[0].RouteCost == nil || len(status.Sensors) != 1 {
		t.Fatalf("unexpected integration status: %+v", status)
	}
	if status.AmateurRepeaters[0].Name != "W4XYZ" || !strings.Contains(status.AmateurRepeaters[0].Details, "Frequency 146.940") {
		t.Fatalf("amateur repeater details were not normalized: %+v", status.AmateurRepeaters[0])
	}
	if !strings.Contains(status.GMRSRepeaters[0].Details, "Tone 141.3") {
		t.Fatalf("GMRS repeater details were not normalized: %+v", status.GMRSRepeaters[0])
	}
	if status.MeshCoreNodes[0].Kind != "repeater" || !strings.Contains(status.MeshCoreNodes[0].Details, "Battery 92%") {
		t.Fatalf("MeshCore node was not normalized: %+v", status.MeshCoreNodes[0])
	}
	var events strings.Builder
	if err = store.EventLog(&events); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(events.String(), "Bridge closed") {
		t.Fatal("transient feed data entered the event log")
	}
}

func TestIntegrationSettingsRejectUnsafeAndExcessEndpoints(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Integrations.AREDNNodeURLs = "file:///etc/passwd"
	if _, err = store.UpdateSettings(settings); err == nil {
		t.Fatal("unsafe endpoint accepted")
	}
	settings.Integrations.AREDNNodeURLs = ""
	for index := 0; index < 11; index++ {
		settings.Integrations.AREDNNodeURLs += fmt.Sprintf("http://node-%d.local/status ", index)
	}
	if _, err = store.UpdateSettings(settings); err == nil {
		t.Fatal("excess endpoints accepted")
	}
}

func TestMeshCoreFeedBoundsAndValidation(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nodes" {
			_, _ = io.WriteString(w, `{"version":1,"nodes":[{"id":"bad","name":"Bad location","latitude":95,"longitude":-86},{"id":"mc2","name":"Portable","type":"companion","latitude":35.9,"longitude":-86.6}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"version":1,"nodes":[`+strings.Repeat(`{"id":"node","name":"Node","latitude":35.9,"longitude":-86.6},`, 1000)+`{"id":"extra","name":"Extra","latitude":35.9,"longitude":-86.6}]}`)
	}))
	defer provider.Close()
	service := &IntegrationService{client: provider.Client()}
	items, err := service.fetchMeshCore(context.Background(), provider.URL+"/nodes")
	if err != nil || len(items) != 1 || items[0].ID != "mc2" {
		t.Fatalf("unexpected validated MeshCore nodes: items=%+v err=%v", items, err)
	}
	if _, err = service.fetchMeshCore(context.Background(), provider.URL+"/excess"); err == nil {
		t.Fatal("MeshCore response over the limit was accepted")
	}
}

func TestOSMRepeaterLookupNormalizesTaggedNodes(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("data"), "around:160934,35.950000,-86.670000") {
			t.Fatalf("unexpected Overpass query: %s", r.URL.Query().Get("data"))
		}
		_, _ = io.WriteString(w, `{"elements":[{"type":"node","id":42,"lat":35.9,"lon":-86.6,"tags":{"communication:amateur_radio:callsign":"W4TEST","communication:amateur_radio:repeater:frequency_out":"146940000","communication:amateur_radio:repeater:ctcss":"123.0"}}]}`)
	}))
	defer provider.Close()
	original := osmRepeaterEndpoint
	osmRepeaterEndpoint = provider.URL
	t.Cleanup(func() { osmRepeaterEndpoint = original })
	service := &IntegrationService{client: provider.Client()}
	items, err := service.fetchOSMRepeaters(context.Background(), 35.95, -86.67)
	if err != nil || len(items) != 1 || items[0].Name != "W4TEST" || items[0].Source != "OpenStreetMap contributors" || !strings.Contains(items[0].Details, "146940000") {
		t.Fatalf("unexpected OSM repeater result: items=%+v err=%v", items, err)
	}
}
