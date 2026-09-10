package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNetworkSettingsPersistHostRoleAndGeneratedKey(t *testing.T) {
	dataDir := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	runtime, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	if active := runtime.Active(); active.Mode != networkModeStandalone || active.ListenPort != 8787 {
		t.Fatalf("unexpected defaults: %+v", active)
	}

	status, err := runtime.Update(NetworkSettingsInput{Mode: networkModeHost, ListenPort: 9876})
	if err != nil {
		t.Fatal(err)
	}
	if !status.RestartRequired || status.Configured.Mode != networkModeHost ||
		!validLANAccessKey(status.Configured.AccessKey) {
		t.Fatalf("unexpected saved host settings: %+v", status)
	}

	reopened, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	active := reopened.Active()
	if active.Mode != networkModeHost || active.ListenPort != 9876 ||
		active.AccessKey != status.Configured.AccessKey {
		t.Fatalf("host settings did not persist: %+v", active)
	}
	if filepath.Base(reopened.path) != "network.json" {
		t.Fatalf("unexpected network settings path: %s", reopened.path)
	}
}

func TestHostLANRequestsRequireAccessKey(t *testing.T) {
	dataDir := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	configurator, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := configurator.Update(NetworkSettingsInput{Mode: networkModeHost, ListenPort: 8787})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, logger)
	if err != nil {
		t.Fatal(err)
	}
	apiServer.network = runtime
	handler := apiServer.Handler()

	request := httptest.NewRequest(http.MethodGet, "http://host/api/state", nil)
	request.RemoteAddr = "192.168.1.22:51234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated LAN status = %d, want 401", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "http://host/api/state", nil)
	request.RemoteAddr = "192.168.1.22:51234"
	request.Header.Set(lanKeyHeader, saved.Configured.AccessKey)
	request.Header.Set(lanClientHeader, "Dispatch Laptop")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated LAN status = %d, body=%s", response.Code, response.Body.String())
	}
	clients := runtime.Status().Clients
	if len(clients) != 1 || clients[0].Name != "Dispatch Laptop" || clients[0].Address != "192.168.1.22" {
		t.Fatalf("authenticated client was not recorded: %+v", clients)
	}
	responder, err := store.CreateResponder(ResponderInput{Name: "OwnTracks Phone", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	ownTracksBody := `{"_type":"location","lat":35.97,"lon":-86.66}`
	request = httptest.NewRequest(http.MethodPost, "http://host/api/tracking/owntracks/"+responder.ID, strings.NewReader(ownTracksBody))
	request.RemoteAddr = "192.168.1.44:51234"
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("phone", saved.Configured.AccessKey)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("OwnTracks Basic authentication status = %d, body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "http://host/api/tracking/opengts/"+responder.ID+"?lat=35.98&lon=-86.67", nil)
	request.RemoteAddr = "192.168.1.45:51234"
	request.SetBasicAuth("tracker", saved.Configured.AccessKey)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("OpenGTS Basic authentication status = %d, body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://host/api/state", nil)
	request.RemoteAddr = "127.0.0.1:51234"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("host-local request status = %d, want 200", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "http://host/api/shutdown", strings.NewReader(`{"confirm":true}`))
	request.RemoteAddr = "192.168.1.22:51234"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Tickets-Local-Shutdown", "confirm")
	request.Header.Set(lanKeyHeader, saved.Configured.AccessKey)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("remote Host shutdown status = %d, want 403", response.Code)
	}
}

func TestClientProxiesOperationalAPIAndKeepsNetworkSettingsLocal(t *testing.T) {
	const accessKey = "abcdefghijklmnop12345678"
	type observedRequest struct {
		Path       string
		Method     string
		AccessKey  string
		ClientName string
	}
	observed := make(chan observedRequest, 4)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- observedRequest{
			Path:       r.URL.Path,
			Method:     r.Method,
			AccessKey:  r.Header.Get(lanKeyHeader),
			ClientName: r.Header.Get(lanClientHeader),
		}
		switch r.URL.Path {
		case "/api/state":
			writeJSON(w, http.StatusOK, State{SchemaVersion: 3})
		case "/api/responders":
			writeJSON(w, http.StatusCreated, Responder{ID: "unit-host", Name: "Host Unit"})
		case "/api/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: change\ndata: {}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer host.Close()

	dataDir := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	configurator, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configurator.Update(NetworkSettingsInput{
		Mode:       networkModeClient,
		HostURL:    host.URL,
		AccessKey:  accessKey,
		ListenPort: 8787,
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, logger)
	if err != nil {
		t.Fatal(err)
	}
	apiServer.network = runtime
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	state := requestJSON[State](t, server.URL+"/api/state", http.MethodGet, nil, http.StatusOK)
	if state.SchemaVersion != 3 {
		t.Fatalf("proxied state schema = %d", state.SchemaVersion)
	}
	responder := requestJSON[Responder](t, server.URL+"/api/responders", http.MethodPost, ResponderInput{
		Name: "Client Unit", Status: "available",
	}, http.StatusCreated)
	if responder.ID != "unit-host" {
		t.Fatalf("response did not come from host: %+v", responder)
	}
	response, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	eventBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(eventBody), "event: change") {
		t.Fatalf("SSE response was not proxied: %s", eventBody)
	}

	for index, want := range []struct {
		path   string
		method string
	}{
		{"/api/state", http.MethodGet},
		{"/api/responders", http.MethodPost},
		{"/api/events", http.MethodGet},
	} {
		got := <-observed
		if got.Path != want.path || got.Method != want.method || got.AccessKey != accessKey || got.ClientName == "" {
			t.Fatalf("proxied request %d = %+v", index, got)
		}
	}

	network := requestJSON[NetworkStatus](t, server.URL+"/api/network/settings", http.MethodGet, nil, http.StatusOK)
	if network.ActiveMode != networkModeClient || network.Configured.HostURL != host.URL {
		t.Fatalf("local network endpoint was unexpectedly proxied: %+v", network)
	}
}

func TestClientConnectionTestValidatesRoleVersionAndKey(t *testing.T) {
	const accessKey = "abcdefghijklmnop12345678"
	var requests atomic.Int32
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get(lanKeyHeader) != accessKey {
			writeError(w, http.StatusUnauthorized, "bad key")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"product": productName,
			"version": version,
			"status":  "ok",
			"role":    networkModeHost,
		})
	}))
	defer host.Close()
	runtime, err := OpenNetworkRuntime(t.TempDir(), 0, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Test(context.Background(), NetworkSettingsInput{
		Mode:       networkModeClient,
		HostURL:    host.URL,
		AccessKey:  accessKey,
		ListenPort: 8787,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Connected || result.HostVersion != version {
		t.Fatalf("unexpected connection test: %+v", result)
	}
	if requests.Load() != 1 {
		t.Fatalf("connection test made %d requests, want exactly one", requests.Load())
	}
}

func TestActiveClientConnectionSettingsApplyWithoutRestart(t *testing.T) {
	const firstKey = "abcdefghijklmnop12345678"
	const secondKey = "zyxwvutsrqponmlk87654321"
	firstHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"source": "first"})
	}))
	defer firstHost.Close()
	secondHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(lanKeyHeader) != secondKey {
			writeError(w, http.StatusUnauthorized, "wrong key")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"source": "second"})
	}))
	defer secondHost.Close()

	dataDir := t.TempDir()
	if err := writeNetworkSettings(filepath.Join(dataDir, "network.json"), NetworkSettings{
		Mode: networkModeClient, HostURL: firstHost.URL, AccessKey: firstKey, ListenPort: 8787,
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := OpenNetworkRuntime(dataDir, 0, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	status, err := runtime.Update(NetworkSettingsInput{
		Mode: networkModeClient, HostURL: secondHost.URL, AccessKey: secondKey, ListenPort: 8787,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.RestartRequired {
		t.Fatalf("same-role Client connection update unexpectedly requires restart: %+v", status)
	}
	if active := runtime.Active(); active.HostURL != secondHost.URL || active.AccessKey != secondKey {
		t.Fatalf("Client connection was not applied immediately: %+v", active)
	}

	handler := runtime.Route(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"source": "local"})
	}))
	request := httptest.NewRequest(http.MethodGet, "http://client/api/state", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || body["source"] != "second" {
		t.Fatalf("updated Client proxy response = %d %s", response.Code, response.Body.String())
	}
}

func TestActiveHostKeyAppliesWithoutRestart(t *testing.T) {
	const firstKey = "abcdefghijklmnop12345678"
	const secondKey = "zyxwvutsrqponmlk87654321"
	dataDir := t.TempDir()
	if err := writeNetworkSettings(filepath.Join(dataDir, "network.json"), NetworkSettings{
		Mode: networkModeHost, AccessKey: firstKey, ListenPort: 8787,
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := OpenNetworkRuntime(dataDir, 0, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	status, err := runtime.Update(NetworkSettingsInput{
		Mode: networkModeHost, AccessKey: secondKey, ListenPort: 8787,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.RestartRequired || runtime.Active().AccessKey != secondKey {
		t.Fatalf("Host key was not applied immediately: %+v", status)
	}

	handler := runtime.Route(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		key  string
		want int
	}{{firstKey, http.StatusUnauthorized}, {secondKey, http.StatusNoContent}} {
		request := httptest.NewRequest(http.MethodGet, "http://host/api/state", nil)
		request.RemoteAddr = "192.0.2.20:50000"
		request.Header.Set(lanKeyHeader, test.key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("key %q response = %d, want %d", test.key, response.Code, test.want)
		}
	}
}

func TestTwoClientsShareHostStateAndConflictProtection(t *testing.T) {
	const accessKey = "abcdefghijklmnop12345678"
	logger := log.New(io.Discard, "", 0)
	hostData := t.TempDir()
	hostConfigurator, err := OpenNetworkRuntime(hostData, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostConfigurator.Update(NetworkSettingsInput{
		Mode: networkModeHost, ListenPort: 8787, AccessKey: accessKey,
	}); err != nil {
		t.Fatal(err)
	}
	hostRuntime, err := OpenNetworkRuntime(hostData, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	hostStore, err := OpenStore(hostData)
	if err != nil {
		t.Fatal(err)
	}
	hostAPI, err := newAPIServer(hostStore, webAssets, logger)
	if err != nil {
		t.Fatal(err)
	}
	hostAPI.network = hostRuntime
	hostHandler := hostAPI.Handler()
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exercise LAN authentication even though httptest connects over loopback.
		switch r.Header.Get(lanClientHeader) {
		case "Dispatch One":
			r.RemoteAddr = "192.0.2.11:50001"
		case "Dispatch Two":
			r.RemoteAddr = "192.0.2.12:50002"
		default:
			r.RemoteAddr = "192.0.2.10:50000"
		}
		hostHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(host.Close)

	newClient := func(name string) *httptest.Server {
		dataDir := t.TempDir()
		configurator, openErr := OpenNetworkRuntime(dataDir, 0, logger)
		if openErr != nil {
			t.Fatal(openErr)
		}
		if _, updateErr := configurator.Update(NetworkSettingsInput{
			Mode: networkModeClient, HostURL: host.URL, AccessKey: accessKey, ListenPort: 8787,
		}); updateErr != nil {
			t.Fatal(updateErr)
		}
		runtime, openErr := OpenNetworkRuntime(dataDir, 0, logger)
		if openErr != nil {
			t.Fatal(openErr)
		}
		runtime.clientName = name
		store, openErr := OpenStore(dataDir)
		if openErr != nil {
			t.Fatal(openErr)
		}
		apiServer, openErr := newAPIServer(store, webAssets, logger)
		if openErr != nil {
			t.Fatal(openErr)
		}
		apiServer.network = runtime
		server := httptest.NewServer(apiServer.Handler())
		t.Cleanup(server.Close)
		return server
	}
	clientOne := newClient("Dispatch One")
	clientTwo := newClient("Dispatch Two")

	created := requestJSON[Responder](t, clientOne.URL+"/api/responders", http.MethodPost, ResponderInput{
		Name: "Medic 4", Status: "available",
	}, http.StatusCreated)
	shared := requestJSON[State](t, clientTwo.URL+"/api/state", http.MethodGet, nil, http.StatusOK)
	if len(shared.Responders) != 1 || shared.Responders[0].ID != created.ID {
		t.Fatalf("second client did not receive Host state: %+v", shared.Responders)
	}

	requestJSON[Responder](t, clientOne.URL+"/api/responders/"+created.ID, http.MethodPut, ResponderInput{
		Name: "Medic 4A", Status: "available", ExpectedUpdatedAt: &created.UpdatedAt,
	}, http.StatusOK)
	conflict := requestJSON[map[string]string](t, clientTwo.URL+"/api/responders/"+created.ID, http.MethodPut, ResponderInput{
		Name: "Medic 4B", Status: "available", ExpectedUpdatedAt: &created.UpdatedAt,
	}, http.StatusConflict)
	if !strings.Contains(conflict["error"], "changed on another computer") {
		t.Fatalf("unexpected cross-client conflict response: %+v", conflict)
	}

	clients := hostRuntime.Status().Clients
	if len(clients) != 2 {
		t.Fatalf("Host recorded %d clients, want 2: %+v", len(clients), clients)
	}
	seen := map[string]bool{}
	for _, client := range clients {
		seen[client.Name] = true
	}
	if !seen["Dispatch One"] || !seen["Dispatch Two"] {
		t.Fatalf("Host client identities are incomplete: %+v", clients)
	}

	host.Close()
	unavailable := requestJSON[map[string]string](t, clientTwo.URL+"/api/state", http.MethodGet, nil, http.StatusBadGateway)
	if !strings.Contains(unavailable["error"], "host is unavailable") {
		t.Fatalf("unexpected Host outage response: %+v", unavailable)
	}
	replayed, err := OpenStore(hostData)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed.Snapshot().Responders) != 1 || replayed.Snapshot().Responders[0].Name != "Medic 4A" {
		t.Fatalf("Host state did not survive replay after simulated outage: %+v", replayed.Snapshot().Responders)
	}
}

func TestClientConnectionTestDoesNotForwardKeyThroughRedirect(t *testing.T) {
	const accessKey = "abcdefghijklmnop12345678"
	receivedKey := make(chan string, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey <- r.Header.Get(lanKeyHeader)
		writeJSON(w, http.StatusOK, map[string]string{
			"product": productName,
			"version": version,
			"status":  "ok",
			"role":    networkModeHost,
		})
	}))
	defer redirectTarget.Close()
	redirectingHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/healthz", http.StatusTemporaryRedirect)
	}))
	defer redirectingHost.Close()

	runtime, err := OpenNetworkRuntime(t.TempDir(), 0, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Test(context.Background(), NetworkSettingsInput{
		Mode:       networkModeClient,
		HostURL:    redirectingHost.URL,
		AccessKey:  accessKey,
		ListenPort: 8787,
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("redirecting connection test error = %v, want HTTP 307 rejection", err)
	}
	select {
	case key := <-receivedKey:
		t.Fatalf("LAN access key reached redirect destination: %q", key)
	default:
	}
}

func TestConcurrentRecordUpdateReturnsConflict(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	responder, err := store.CreateResponder(ResponderInput{Name: "Unit", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	first := ResponderInput{
		Name:              "Unit from Client A",
		Status:            "available",
		ExpectedUpdatedAt: &responder.UpdatedAt,
	}
	if _, err := store.UpdateResponder(responder.ID, first); err != nil {
		t.Fatal(err)
	}
	second := ResponderInput{
		Name:              "Unit from Client B",
		Status:            "available",
		ExpectedUpdatedAt: &responder.UpdatedAt,
	}
	if _, err := store.UpdateResponder(responder.ID, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v, want ErrConflict", err)
	}
}

func TestStaleAPIUpdateReturnsHTTPConflict(t *testing.T) {
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
		Name: "Original", Status: "available",
	}, http.StatusCreated)
	requestJSON[Responder](t, server.URL+"/api/responders/"+responder.ID, http.MethodPut, ResponderInput{
		Name: "First update", Status: "available", ExpectedUpdatedAt: &responder.UpdatedAt,
	}, http.StatusOK)
	result := requestJSON[map[string]string](t, server.URL+"/api/responders/"+responder.ID, http.MethodPut, ResponderInput{
		Name: "Stale update", Status: "available", ExpectedUpdatedAt: &responder.UpdatedAt,
	}, http.StatusConflict)
	if !strings.Contains(result["error"], "changed on another computer") {
		t.Fatalf("unexpected conflict response: %+v", result)
	}
}

func TestNetworkSettingsJSONDoesNotEnterOperationalState(t *testing.T) {
	dataDir := t.TempDir()
	runtime, err := OpenNetworkRuntime(dataDir, 0, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	status, err := runtime.Update(NetworkSettingsInput{Mode: networkModeHost, ListenPort: 8787})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "network.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings NetworkSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.AccessKey != status.Configured.AccessKey {
		t.Fatal("saved network key did not match the configured key")
	}
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	backup := new(strings.Builder)
	if err := store.EventLog(backup); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(backup.String(), settings.AccessKey) {
		t.Fatal("LAN access key leaked into the operational event log")
	}
}
