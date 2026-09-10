package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPILifecycle(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	responder := requestJSON[Responder](t, server.URL+"/api/responders", http.MethodPost, ResponderInput{
		Name: "Rescue 1", Callsign: "R1", Type: "Search", Status: "available",
	}, http.StatusCreated)
	incident := requestJSON[Incident](t, server.URL+"/api/incidents", http.MethodPost, IncidentInput{
		Title: "Lost hiker", Type: "Search & Rescue", Severity: "high", Status: "new", Address: "North trailhead",
	}, http.StatusCreated)
	assigned := requestJSON[Incident](t, server.URL+"/api/incidents/"+incident.ID+"/assignments", http.MethodPost, map[string]string{
		"responder_id": responder.ID,
	}, http.StatusCreated)
	if len(assigned.Assignments) != 1 || assigned.Status != "assigned" {
		t.Fatalf("unexpected assigned incident: %+v", assigned)
	}

	state := requestJSON[State](t, server.URL+"/api/state", http.MethodGet, nil, http.StatusOK)
	if len(state.Incidents) != 1 || len(state.Responders) != 1 {
		t.Fatalf("unexpected API state: %+v", state)
	}

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", response.StatusCode)
	}
	contentSecurityPolicy := response.Header.Get("Content-Security-Policy")
	if !strings.Contains(contentSecurityPolicy, "frame-ancestors 'none'") {
		t.Fatal("security headers are missing")
	}
	if !strings.Contains(contentSecurityPolicy, "img-src 'self' data: blob: https://tile.openstreetmap.org") {
		t.Fatal("content security policy does not allow proxied radar object URLs")
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("index cache control = %q, want no-store", response.Header.Get("Cache-Control"))
	}
	indexBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	indexHTML := string(indexBody)
	if !strings.Contains(indexHTML, `data-map-window href="/?view=map" target="tickets-local-situation-map"`) {
		t.Fatal("index is missing the detached full-screen map link")
	}
	if !strings.Contains(indexHTML, `data-map-position aria-label="Map position on Situation page"`) {
		t.Fatal("Situation map position selector is missing")
	}
	if !strings.Contains(indexHTML, `id="network-refresh-addresses"`) ||
		!strings.Contains(indexHTML, `Test &amp; save connection`) {
		t.Fatal("index is missing the refreshable, save-on-test LAN controls")
	}
	if !strings.Contains(indexHTML, `id="water-site-search"`) ||
		!strings.Contains(indexHTML, `id="water-site-results"`) ||
		!strings.Contains(indexHTML, `public USGS service`) {
		t.Fatal("index is missing the disclosed nearby water-gauge finder")
	}
	if !strings.Contains(indexHTML, `id="water-site-search"`) ||
		!strings.Contains(indexHTML, `id="water-site-results"`) ||
		!strings.Contains(indexHTML, "public USGS service") {
		t.Fatal("water settings are missing nearby named-gauge discovery and its location disclosure")
	}
	for _, disclosure := range []string{
		`data-page="about"`,
		`Version 0.5.6`,
		`AI-assisted hobby project`,
		`provided without warranty of any kind`,
		`href="https://www.openstreetmap.org/copyright"`,
		`data available under ODbL`,
		`NOAA/National Weather Service and USGS names identify enabled public data sources and do not imply endorsement`,
	} {
		if !strings.Contains(indexHTML, disclosure) {
			t.Fatalf("About, warranty, or attribution disclosure is missing %q", disclosure)
		}
	}
	script, err := http.Get(server.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer script.Body.Close()
	if script.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("app script cache control = %q, want no-store", script.Header.Get("Cache-Control"))
	}
	scriptBody, err := io.ReadAll(script.Body)
	if err != nil {
		t.Fatal(err)
	}
	scriptText := string(scriptBody)
	if !strings.Contains(scriptText, `localStorage.setItem("tickets-local-map-position", select.value)`) ||
		!strings.Contains(scriptText, `function applyMapPosition(position)`) {
		t.Fatal("Situation map position is not persisted locally")
	}
	for _, guard := range []string{
		"settingsFormDirty.general", "settingsFormDirty.aprs", "settingsFormDirty.weather",
		"settingsFormDirty.water", "settingsFormDirty.integrations", "settingsFormDirty.network",
	} {
		if !strings.Contains(scriptText, guard) {
			t.Fatalf("app script is missing the %s draft guard", guard)
		}
	}
	if !strings.Contains(scriptText, "refreshNetworkAddresses") {
		t.Fatal("app script is missing LAN draft preservation or address refresh behavior")
	}
	styles, err := http.Get(server.URL + "/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	defer styles.Body.Close()
	stylesBody, err := io.ReadAll(styles.Body)
	if err != nil {
		t.Fatal(err)
	}
	stylesText := string(stylesBody)
	if !strings.Contains(stylesText, ".map-drawing-control { position: absolute; z-index: 9; left: 12px; bottom: 82px;") ||
		!strings.Contains(stylesText, "max-width: calc(100% - 76px)") {
		t.Fatal("map drawing toolbar is not separated from the weather and help overlays")
	}
	if !strings.Contains(scriptText, `async deleteDrawingFromEvent(event)`) ||
		!strings.Contains(scriptText, `shape.dataset.drawnOverlayId = overlay.id`) ||
		!strings.Contains(stylesText, `.map-drawn-overlay { pointer-events: visiblePainted; cursor: pointer; }`) {
		t.Fatal("saved map drawings are not directly selectable for deletion")
	}
	if !strings.Contains(indexHTML, `data-map-manage-overlays>Manage drawings</button>`) ||
		!strings.Contains(indexHTML, `id="overlay-settings"`) ||
		!strings.Contains(scriptText, `data-overlay-action="delete" aria-label="Delete ${attr(overlay.name)}">Delete</button>`) ||
		!strings.Contains(scriptText, `Click a shape to delete · Manage drawings for a list`) {
		t.Fatal("saved drawings do not expose an obvious map and list deletion workflow")
	}
	if !strings.Contains(scriptText, `else toast(mapPointKindLabel(point.kind), point.label)`) ||
		!strings.Contains(scriptText, `polygon.dataset.weatherAlertLabel = alertLabel`) ||
		!strings.Contains(stylesText, `.weather-alert-polygon[data-weather-alert-label] { pointer-events: visiblePainted; cursor: pointer; }`) {
		t.Fatal("informational map markers or warning polygons are not directly selectable for details")
	}
	for _, layer := range []string{
		"[hidden] { display: none !important; }",
		".map-grid-fallback { z-index: 0; }",
		".map-tiles { z-index: 1; }",
		".map-radar { z-index: 6; display: block;",
		".map-trails { z-index: 7;",
		".map-markers { z-index: 7; pointer-events: none; }",
	} {
		if !strings.Contains(stylesText, layer) {
			t.Fatalf("map stylesheet is missing explicit layer ordering: %s", layer)
		}
	}
	if !strings.Contains(scriptText, `this.layers.aprs === false ? [] : (state.tracks || [])`) {
		t.Fatal("APRS trail rendering does not follow the APRS layer toggle")
	}
	if !strings.Contains(scriptText, `state.weather_alerts = app.weather?.alerts || app.state.weather_alerts || [];`) {
		t.Fatal("state refreshes do not preserve weather warning polygons")
	}
	if !strings.Contains(scriptText, `function waterGaugeFloodLevel(gauge)`) ||
		!strings.Contains(scriptText, "statusClass:`flood-${waterGaugeFloodLevel(item)}`") ||
		!strings.Contains(stylesText, `.map-marker.water.flood-major { --marker: #a855f7; }`) ||
		!strings.Contains(stylesText, `.water-gauge.flood-moderate { border-color: #ef4444; }`) {
		t.Fatal("river gauges are not color-coded by observed or forecast flood category")
	}
	if !strings.Contains(scriptText, `async function discoverWaterSites()`) ||
		!strings.Contains(scriptText, `function addDiscoveredWaterSites()`) {
		t.Fatal("water settings cannot discover and select named nearby gauges")
	}
	if count := strings.Count(scriptText, `$("#message-form").addEventListener("submit", saveMessage);`); count != 1 {
		t.Fatalf("message form has %d submit bindings, want exactly one", count)
	}
	if !strings.Contains(scriptText, `settings.radar_animation && localRadarURL`) ||
		!strings.Contains(scriptText, `this.radarLayer.hidden = true;`) ||
		!strings.Contains(scriptText, `if (this.radarPendingKey) return;`) {
		t.Fatal("public NOAA radar does not use the fast current-frame path or clear misaligned frames")
	}
	if !strings.Contains(scriptText, `async function discoverWaterSites()`) ||
		!strings.Contains(scriptText, `function addDiscoveredWaterSites()`) {
		t.Fatal("water settings are missing gauge discovery behavior")
	}
	if !strings.Contains(scriptText, "this.radarStatus.textContent = `${stale ? \"Cached radar\" : \"Radar\"} as of ${label}`;") ||
		!strings.Contains(stylesText, ".map-radar-status { position: absolute;") ||
		!strings.Contains(stylesText, "[hidden] { display: none !important; }") {
		t.Fatal("enabled radar layer does not display its observation timestamp")
	}

	response = rawRequest(t, server.URL+"/api/responders", http.MethodPost, []byte(`{"name":"Bad","status":"available","unexpected":true}`))
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/api/not-real")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown API status = %d, want 404", response.StatusCode)
	}
}

func TestBackupEndpoint(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateResponder(ResponderInput{Name: "Unit", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"responder.created"`)) {
		t.Fatalf("unexpected backup response: status=%d body=%s", response.StatusCode, body)
	}
	if !strings.Contains(response.Header.Get("Content-Disposition"), "tickets-local-backup-") {
		t.Fatal("backup filename is missing")
	}
}

func TestAPICreatesMultipleResponders(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	first := requestJSON[Responder](t, server.URL+"/api/responders", http.MethodPost, ResponderInput{
		Name: "Medic 1", Callsign: "MEDIC-1", Status: "available",
	}, http.StatusCreated)
	second := requestJSON[Responder](t, server.URL+"/api/responders", http.MethodPost, ResponderInput{
		Name: "Search 2", Callsign: "SEARCH-2", Status: "available",
	}, http.StatusCreated)
	if first.ID == second.ID {
		t.Fatalf("responders received the same ID %q", first.ID)
	}

	state := requestJSON[State](t, server.URL+"/api/state", http.MethodGet, nil, http.StatusOK)
	if len(state.Responders) != 2 {
		t.Fatalf("responder count = %d, want 2: %+v", len(state.Responders), state.Responders)
	}
}

func TestAPIResponderSaveAndReconnectNotifyAPRSManager(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	apiServer.aprs = manager
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	responder := requestJSON[Responder](t, server.URL+"/api/responders", http.MethodPost, ResponderInput{
		Name: "Tracker", Callsign: "N0CALL-7", Status: "available", APRSEnabled: true,
	}, http.StatusCreated)
	afterCreate := manager.generation.Load()
	if afterCreate == 0 {
		t.Fatal("creating an APRS responder did not notify the APRS manager")
	}

	requestJSON[Responder](t, server.URL+"/api/responders/"+responder.ID, http.MethodPut, ResponderInput{
		Name: "Tracker Updated", Callsign: "N0CALL-7", Status: "available", APRSEnabled: true,
	}, http.StatusOK)
	afterUpdate := manager.generation.Load()
	if afterUpdate <= afterCreate {
		t.Fatal("updating an APRS responder did not notify the APRS manager")
	}

	requestJSON[APRSStatus](t, server.URL+"/api/aprs/reconnect", http.MethodPost, map[string]string{}, http.StatusAccepted)
	if afterReconnect := manager.generation.Load(); afterReconnect <= afterUpdate {
		t.Fatal("explicit APRS reconnect did not notify the APRS manager")
	}
}

func TestAPIStateIncludesTransientAPRSAreaStations(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.AreaEnabled = true
	settings.APRS.AreaRadiusMiles = 25
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	manager.recordActivity(APRSPosition{
		Callsign:   "N1AREA-9",
		Latitude:   35.9,
		Longitude:  -86.7,
		ReceivedAt: time.Now().UTC(),
	}, "aprs_is")
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	apiServer.aprs = manager
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	state := requestJSON[State](t, server.URL+"/api/state", http.MethodGet, nil, http.StatusOK)
	if len(state.APRSStations) != 1 || state.APRSStations[0].Callsign != "N1AREA-9" {
		t.Fatalf("unexpected APRS area stations: %+v", state.APRSStations)
	}
	if state.APRSStatus.AreaStations != 1 {
		t.Fatalf("APRS area station count = %d, want 1", state.APRSStatus.AreaStations)
	}
	if len(store.Snapshot().APRSStations) != 0 {
		t.Fatal("transient APRS activity leaked into the persisted store state")
	}
}

func TestAPISettingsAcceptsPasscodeWithoutReturningIt(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	settings := store.Snapshot().Settings
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.Passcode = "12345"
	saved := requestJSON[Settings](t, server.URL+"/api/settings", http.MethodPut, settings, http.StatusOK)
	if saved.APRS.Passcode != "" {
		t.Fatal("settings response returned the APRS-IS passcode")
	}
	if !saved.APRS.PasscodeConfigured || store.APRSPasscode() != "12345" {
		t.Fatal("settings request did not save the APRS-IS passcode")
	}
}

func TestAPIShutdownRequiresConfirmationAndRequestsQuit(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{}, 1)
	apiServer.requestQuit = func() { quit <- struct{}{} }
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	response := rawRequest(t, server.URL+"/api/shutdown", http.MethodPost, []byte(`{"confirm":true}`))
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("shutdown without confirmation header status=%d, want 403", response.StatusCode)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/shutdown", strings.NewReader(`{"confirm":true}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Tickets-Local-Shutdown", "confirm")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("confirmed shutdown status=%d, want 202; body=%s", response.StatusCode, data)
	}

	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("confirmed shutdown did not request application quit")
	}
}

func TestAPILocationsAndKMLOverlay(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	first := requestJSON[Location](t, server.URL+"/api/locations", http.MethodPost, LocationInput{
		Name: "North Staging", Address: "North gate",
	}, http.StatusCreated)
	second := requestJSON[Location](t, server.URL+"/api/locations", http.MethodPost, LocationInput{
		Name: "South Staging", Address: "South gate",
	}, http.StatusCreated)
	if first.ID == second.ID {
		t.Fatalf("locations received the same ID %q", first.ID)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "route.kml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(file, `<kml xmlns="http://www.opengis.net/kml/2.2"><Document><name>Route Plan</name><Placemark><LineString><coordinates>-86.7,35.9 -86.8,36.0</coordinates></LineString></Placemark></Document></kml>`)
	_ = writer.WriteField("color", "#3366FF")
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/overlays/import", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("KML import status=%d body=%s", response.StatusCode, data)
	}

	state := requestJSON[State](t, server.URL+"/api/state", http.MethodGet, nil, http.StatusOK)
	if len(state.Locations) != 2 || len(state.Overlays) != 1 {
		t.Fatalf("unexpected map state: locations=%+v overlays=%+v", state.Locations, state.Overlays)
	}
	if state.Overlays[0].Name != "Route Plan" || state.Overlays[0].Color != "#3366FF" {
		t.Fatalf("unexpected imported overlay: %+v", state.Overlays[0])
	}

	requestJSON[map[string]string](t, server.URL+"/api/locations/"+first.ID, http.MethodDelete, nil, http.StatusOK)
	requestJSON[map[string]string](t, server.URL+"/api/overlays/"+state.Overlays[0].ID, http.MethodDelete, nil, http.StatusOK)
	state = requestJSON[State](t, server.URL+"/api/state", http.MethodGet, nil, http.StatusOK)
	if len(state.Locations) != 1 || len(state.Overlays) != 0 {
		t.Fatalf("delete endpoints did not update state: %+v", state)
	}
}

func TestAPIGeocodeReturnsUsableCoordinates(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("provider path = %q, want /search", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "100 Main Street, Franklin, TN" {
			t.Errorf("provider query = %q", got)
		}
		if r.URL.Query().Get("viewbox") == "" || r.URL.Query().Get("bounded") != "0" {
			t.Error("map-center search bias was not supplied")
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "github.com/openises/tickets") {
			t.Errorf("provider User-Agent = %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Accept-Language") == "" {
			t.Error("provider Accept-Language is missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"display_name":"100 Main Street, Franklin, Tennessee, USA","lat":"35.925100","lon":"-86.868900","type":"house"}]`)
	}))
	defer provider.Close()

	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	apiServer.geocodeURL = provider.URL + "/search"
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	type result struct {
		DisplayName string  `json:"display_name"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	results := requestJSON[[]result](
		t,
		server.URL+"/api/geocode?q=100%20Main%20Street%2C%20Franklin%2C%20TN",
		http.MethodGet,
		nil,
		http.StatusOK,
	)
	if len(results) != 1 || results[0].Latitude != 35.9251 || results[0].Longitude != -86.8689 {
		t.Fatalf("unexpected geocode results: %+v", results)
	}
}

func TestParseIntersectionFormats(t *testing.T) {
	for _, query := range []string{
		"Main St & First Ave, Franklin, TN",
		"Main St and First Ave, Franklin, TN",
		"Main St at First Ave, Franklin, TN",
		"Main St / First Ave, Franklin, TN",
		"Main St @ First Ave, Franklin, TN",
	} {
		road1, road2, place, ok := parseIntersection(query)
		if !ok || road1 != "Main St" || road2 != "First Ave" || place != "Franklin, TN" {
			t.Fatalf("parseIntersection(%q) = %q, %q, %q, %v", query, road1, road2, place, ok)
		}
	}
	if _, _, _, ok := parseIntersection("100 Main Street, Franklin, TN"); ok {
		t.Fatal("ordinary address was classified as an intersection")
	}
}

func TestAPIGeocodeFallsBackToCrossStreetLookup(t *testing.T) {
	nominatim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer nominatim.Close()

	overpass := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Overpass method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		query := r.Form.Get("data")
		for _, want := range []string{`node(w.a)(w.b)`, `(St|Street)`, `(Ave|Avenue)`, `[[:space:]]+`} {
			if !strings.Contains(query, want) {
				t.Errorf("Overpass query %q does not contain %q", query, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"elements":[{"type":"node","id":42,"lat":35.9251,"lon":-86.8689}]}`)
	}))
	defer overpass.Close()

	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	apiServer.geocodeURL = nominatim.URL
	apiServer.overpassURL = overpass.URL
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	results := requestJSON[[]geocodeResult](t, server.URL+"/api/geocode?q=Main%20St%20%26%20First%20Ave%2C%20Franklin%2C%20TN", http.MethodGet, nil, http.StatusOK)
	if len(results) != 1 || results[0].Type != "intersection" || results[0].Latitude != 35.9251 || results[0].DisplayName != "Main St & First Ave, Franklin, TN" {
		t.Fatalf("unexpected cross-street results: %+v", results)
	}
}

func requestJSON[T any](t *testing.T, url, method string, body any, expectedStatus int) T {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	response := rawRequest(t, url, method, payload)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != expectedStatus {
		t.Fatalf("%s %s status=%d, want=%d body=%s", method, url, response.StatusCode, expectedStatus, data)
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, data)
	}
	return result
}

func rawRequest(t *testing.T, url, method string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
