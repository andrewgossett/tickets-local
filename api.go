package main

import (
	"archive/zip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type apiServer struct {
	store        *Store
	aprs         *APRSManager
	network      *NetworkRuntime
	requestQuit  func()
	web          fs.FS
	logger       *log.Logger
	client       *http.Client
	geocodeURL   string
	overpassURL  string
	geocodeMu    sync.Mutex
	lastGeocode  time.Time
	quitOnce     sync.Once
	weather      *WeatherService
	water        *WaterService
	integrations *IntegrationService
	mapTiles     *MapTileService
	routing      *RoutingService
	mobile       *MobileAccessStore
}

func newAPIServer(store *Store, assets embed.FS, logger *log.Logger) (*apiServer, error) {
	web, err := fs.Sub(assets, "web")
	if err != nil {
		return nil, err
	}
	geocodeURL := strings.TrimSpace(os.Getenv("TICKETS_LOCAL_GEOCODE_URL"))
	if geocodeURL == "" {
		geocodeURL = "https://nominatim.openstreetmap.org/search"
	}
	overpassURL := strings.TrimSpace(os.Getenv("TICKETS_LOCAL_OVERPASS_URL"))
	if overpassURL == "" {
		overpassURL = "https://overpass-api.de/api/interpreter"
	}
	server := &apiServer{
		store:       store,
		web:         web,
		logger:      logger,
		client:      &http.Client{Timeout: 12 * time.Second},
		geocodeURL:  geocodeURL,
		overpassURL: overpassURL,
	}
	server.weather = NewWeatherService(store, server.client, logger)
	server.water = NewWaterService(store, server.client, logger)
	server.integrations = NewIntegrationService(store, server.client, logger)
	server.mapTiles, err = NewMapTileService(store.dataDir, server.client)
	if err != nil {
		return nil, fmt.Errorf("prepare offline maps: %w", err)
	}
	server.routing = NewRoutingService(server.client)
	return server, nil
}

func (s *apiServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/state", s.state)
	mux.HandleFunc("/api/incidents", s.incidents)
	mux.HandleFunc("/api/incidents/", s.incident)
	mux.HandleFunc("/api/responders", s.responders)
	mux.HandleFunc("/api/responders/", s.responder)
	mux.HandleFunc("/api/facilities", s.facilities)
	mux.HandleFunc("/api/facilities/", s.facility)
	mux.HandleFunc("/api/locations", s.locations)
	mux.HandleFunc("/api/locations/", s.location)
	mux.HandleFunc("/api/assets", s.assets)
	mux.HandleFunc("/api/assets/", s.asset)
	mux.HandleFunc("/api/schedule", s.scheduleItems)
	mux.HandleFunc("/api/schedule/", s.scheduleItem)
	mux.HandleFunc("/api/qualifications", s.qualifications)
	mux.HandleFunc("/api/qualifications/", s.qualification)
	mux.HandleFunc("/api/messages", s.messages)
	mux.HandleFunc("/api/tracking/owntracks/", s.ownTracks)
	mux.HandleFunc("/api/tracking/opengts/", s.openGTS)
	mux.HandleFunc("/api/overlays/import", s.overlayImport)
	mux.HandleFunc("/api/overlays", s.overlays)
	mux.HandleFunc("/api/overlays/", s.overlay)
	mux.HandleFunc("/api/settings", s.settings)
	mux.HandleFunc("/api/aprs/status", s.aprsStatus)
	mux.HandleFunc("/api/aprs/reconnect", s.aprsReconnect)
	mux.HandleFunc("/api/aprs/local/devices", s.aprsLocalDevices)
	mux.HandleFunc("/api/network/settings", s.networkSettings)
	mux.HandleFunc("/api/network/status", s.networkStatus)
	mux.HandleFunc("/api/network/test", s.networkTest)
	mux.HandleFunc("/api/mobile/enroll", s.mobileEnroll)
	mux.HandleFunc("/api/mobile/state", s.mobileState)
	mux.HandleFunc("/api/mobile/status", s.mobileStatus)
	mux.HandleFunc("/api/mobile/admin/enrollment", s.mobileAdminEnrollment)
	mux.HandleFunc("/api/mobile/admin/devices", s.mobileAdminDevices)
	mux.HandleFunc("/api/mobile/admin/devices/", s.mobileAdminDevice)
	mux.HandleFunc("/api/connections", s.connectionDiagnostics)
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/backup", s.backup)
	mux.HandleFunc("/api/backup/archive", s.backupArchive)
	mux.HandleFunc("/api/restore/validate", s.restoreValidate)
	mux.HandleFunc("/api/restore/apply", s.restoreApply)
	mux.HandleFunc("/api/geocode", s.geocode)
	mux.HandleFunc("/api/weather", s.weatherStatus)
	mux.HandleFunc("/api/weather/radar", s.weatherRadar)
	mux.HandleFunc("/api/water", s.waterStatus)
	mux.HandleFunc("/api/water/sites", s.waterSites)
	mux.HandleFunc("/api/integrations", s.integrationStatus)
	mux.HandleFunc("/api/map/tiles/", s.mapTile)
	mux.HandleFunc("/api/map/packs", s.mapPacks)
	mux.HandleFunc("/api/map/packs/", s.mapPack)
	mux.HandleFunc("/api/map/route", s.mapRoute)
	mux.HandleFunc("/api/demo", s.demo)
	mux.HandleFunc("/api/shutdown", s.shutdown)
	mux.HandleFunc("/", s.static)
	var handler http.Handler = mux
	if s.network != nil {
		handler = s.network.Route(handler)
	}
	return s.securityHeaders(s.recoverPanic(s.logRequests(handler)))
}

func (s *apiServer) assets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input AssetInput
	if !decodeJSON(w, r, &input) {
		return
	}
	asset, err := s.store.CreateAsset(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

func (s *apiServer) asset(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/assets/"), "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "asset id is required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input AssetInput
		if !decodeJSON(w, r, &input) {
			return
		}
		asset, err := s.store.UpdateAsset(id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, asset)
	case http.MethodDelete:
		if err := s.store.DeleteAsset(id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

func (s *apiServer) scheduleItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input ScheduleItemInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.store.CreateScheduleItem(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *apiServer) scheduleItem(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/schedule/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input ScheduleItemInput
		if !decodeJSON(w, r, &input) {
			return
		}
		item, err := s.store.UpdateScheduleItem(id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if err := s.store.DeleteScheduleItem(id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

func (s *apiServer) qualifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input QualificationInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.store.CreateQualification(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *apiServer) qualification(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/qualifications/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input QualificationInput
		if !decodeJSON(w, r, &input) {
			return
		}
		item, err := s.store.UpdateQualification(id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		if err := s.store.DeleteQualification(id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

func (s *apiServer) messages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input OperationalMessageInput
	if !decodeJSON(w, r, &input) {
		return
	}
	message, err := s.store.CreateOperationalMessage(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, message)
}

func (s *apiServer) mapRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input RouteInput
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.routing.Route(r.Context(), input)
	if err != nil {
		var validation validationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusBadGateway, "route unavailable: "+err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *apiServer) overlays(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input MapOverlayInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.FileName = "Map drawing"
	overlay, err := s.store.CreateOverlay(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, overlay)
}

func (s *apiServer) mapTile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	zoom, x, y, err := mapTilePathValues(r.URL.Path)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	data, cached, err := s.mapTiles.Tile(r.Context(), zoom, x, y)
	if err != nil {
		var validation validationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusBadGateway, "map tile is unavailable online and is not in local storage")
		}
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Tickets-Map-Cached", strconv.FormatBool(cached))
	_, _ = w.Write(data)
}

func (s *apiServer) mapPacks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.mapTiles.Packs())
	case http.MethodPost:
		var input MapPackInput
		if !decodeJSON(w, r, &input) {
			return
		}
		pack, err := s.mapTiles.CreatePack(r.Context(), input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, pack)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *apiServer) mapPack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/map/packs/"), "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "map pack id is required")
		return
	}
	if err := s.mapTiles.DeletePack(id); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "map pack not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *apiServer) weatherStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.weather == nil {
		writeJSON(w, http.StatusOK, WeatherStatus{State: "disabled", Message: "Weather service is unavailable", Alerts: []WeatherAlert{}})
		return
	}
	writeJSON(w, http.StatusOK, s.weather.Status(r.Context()))
}

func (s *apiServer) waterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.water == nil {
		writeJSON(w, http.StatusOK, WaterStatus{State: "disabled", Message: "Water monitoring is unavailable", Gauges: []WaterGauge{}})
		return
	}
	writeJSON(w, http.StatusOK, s.water.Status(r.Context()))
}

func (s *apiServer) waterSites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.water == nil {
		writeError(w, http.StatusServiceUnavailable, "water gauge search is unavailable")
		return
	}
	latitude, latErr := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	longitude, lonErr := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	radius, radiusErr := strconv.ParseFloat(r.URL.Query().Get("radius"), 64)
	if latErr != nil || lonErr != nil || radiusErr != nil {
		writeError(w, http.StatusBadRequest, "gauge search requires map center coordinates and a radius")
		return
	}
	sites, err := s.water.DiscoverSites(r.Context(), latitude, longitude, radius)
	if err != nil {
		var validation validationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": "USGS Water Services", "sites": sites})
}

func (s *apiServer) integrationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.integrations == nil {
		writeJSON(w, http.StatusOK, emptyIntegrationStatus("disabled", "Integration service unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, s.integrations.Status(r.Context()))
}

func (s *apiServer) weatherRadar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.weather == nil {
		writeError(w, http.StatusServiceUnavailable, "weather service is unavailable")
		return
	}
	image, updatedAt, stale, err := s.weather.Radar(r.Context(), r.URL.Query())
	if err != nil {
		var validation validationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("X-Tickets-Weather-Time", updatedAt.Format(time.RFC3339))
	w.Header().Set("X-Tickets-Weather-Stale", strconv.FormatBool(stale))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(image)
}

func (s *apiServer) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if r.Header.Get("X-Tickets-Local-Shutdown") != "confirm" {
		writeError(w, http.StatusForbidden, "shutdown confirmation header is required")
		return
	}
	var input struct {
		Confirm bool `json:"confirm"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !input.Confirm {
		writeError(w, http.StatusBadRequest, "shutdown confirmation is required")
		return
	}
	if s.requestQuit == nil {
		writeError(w, http.StatusServiceUnavailable, "application shutdown is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
	go func() {
		// Let the accepted response reach the browser before stopping the local
		// listener that carried it.
		time.Sleep(150 * time.Millisecond)
		s.quitOnce.Do(s.requestQuit)
	}()
}

func (s *apiServer) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	role := networkModeStandalone
	if s.network != nil {
		role = s.network.Active().Mode
	}
	writeJSON(w, http.StatusOK, map[string]string{"product": productName, "version": version, "status": "ok", "role": role})
}

func (s *apiServer) networkSettings(w http.ResponseWriter, r *http.Request) {
	if s.network == nil {
		writeError(w, http.StatusServiceUnavailable, "network configuration is unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.network.Status())
	case http.MethodPut:
		var input NetworkSettingsInput
		if !decodeJSON(w, r, &input) {
			return
		}
		status, err := s.network.Update(input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func (s *apiServer) networkStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.network == nil {
		writeError(w, http.StatusServiceUnavailable, "network configuration is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.network.Status())
}

func (s *apiServer) networkTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.network == nil {
		writeError(w, http.StatusServiceUnavailable, "network configuration is unavailable")
		return
	}
	var input NetworkSettingsInput
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.network.Test(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *apiServer) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	snapshot := s.store.Snapshot()
	if s.aprs != nil {
		snapshot.APRSStatus = s.aprs.Status()
		snapshot.APRSStations = s.aprs.Activity()
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *apiServer) incidents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var input IncidentInput
		if !decodeJSON(w, r, &input) {
			return
		}
		incident, err := s.store.CreateIncident(input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, incident)
	default:
		methodNotAllowed(w, http.MethodPost)
	}
}

func (s *apiServer) incident(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/incidents/"))
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]

	if len(parts) == 1 && r.Method == http.MethodPut {
		var input IncidentInput
		if !decodeJSON(w, r, &input) {
			return
		}
		incident, err := s.store.UpdateIncident(id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, incident)
		return
	}

	if len(parts) == 2 && parts[1] == "actions" && r.Method == http.MethodPost {
		var input struct {
			Description string `json:"description"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		incident, err := s.store.AddIncidentAction(id, input.Description)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, incident)
		return
	}
	if len(parts) == 2 && parts[1] == "attachments" && r.Method == http.MethodPost {
		s.uploadAttachment(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "export" && r.Method == http.MethodGet {
		result, err := findIncidentExport(s.store.Snapshot(), id, r.URL.Query().Get("format"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 2 && parts[1] == "winlink-email" && r.Method == http.MethodPost {
		var input WinlinkEmailInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, err := findWinlinkEmail(s.store.Snapshot(), id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 3 && parts[1] == "attachments" && r.Method == http.MethodGet {
		s.downloadAttachment(w, r, id, parts[2])
		return
	}

	if len(parts) == 2 && parts[1] == "assignments" && r.Method == http.MethodPost {
		var input struct {
			ResponderID string `json:"responder_id"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		incident, err := s.store.AssignResponder(id, input.ResponderID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, incident)
		return
	}

	if len(parts) == 3 && parts[1] == "assignments" && r.Method == http.MethodDelete {
		incident, err := s.store.UnassignResponder(id, parts[2])
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, incident)
		return
	}
	methodNotAllowed(w, http.MethodPut, http.MethodPost, http.MethodDelete)
}

func (s *apiServer) uploadAttachment(w http.ResponseWriter, r *http.Request, incidentID string) {
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(11 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "attachment must be 10 MB or smaller")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "attachment file is required")
		return
	}
	defer file.Close()
	name := clean(filepath.Base(header.Filename))
	if name == "" || name == "." {
		writeError(w, http.StatusBadRequest, "attachment filename is invalid")
		return
	}
	head := make([]byte, 512)
	count, _ := io.ReadFull(file, head)
	head = head[:count]
	mediaType := http.DetectContentType(head)
	extensions := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "application/pdf": ".pdf"}
	extension, ok := extensions[mediaType]
	if !ok {
		writeError(w, http.StatusBadRequest, "only JPEG, PNG, and PDF attachments are supported")
		return
	}
	directory := filepath.Join(s.store.dataDir, "attachments", incidentID)
	if err = os.MkdirAll(directory, 0700); err != nil {
		writeError(w, http.StatusInternalServerError, "could not prepare attachment storage")
		return
	}
	attachment := IncidentAttachment{ID: newID("att"), Name: abbreviate(name, 200), MediaType: mediaType, CreatedAt: time.Now().UTC()}
	attachment.StorageName = attachment.ID + extension
	destination := filepath.Join(directory, attachment.StorageName)
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store attachment")
		return
	}
	written, copyErr := output.Write(head)
	if copyErr == nil {
		var n int64
		n, copyErr = io.Copy(output, io.LimitReader(file, (10<<20)-int64(written)+1))
		written += int(n)
	}
	if syncErr := output.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := output.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil || written > 10<<20 {
		_ = os.Remove(destination)
		writeError(w, http.StatusBadRequest, "attachment could not be stored within the 10 MB limit")
		return
	}
	attachment.Size = int64(written)
	incident, err := s.store.AddIncidentAttachment(incidentID, attachment)
	if err != nil {
		_ = os.Remove(destination)
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, incident)
}

func (s *apiServer) downloadAttachment(w http.ResponseWriter, r *http.Request, incidentID, attachmentID string) {
	var found *IncidentAttachment
	for _, incident := range s.store.Snapshot().Incidents {
		if incident.ID != incidentID {
			continue
		}
		for index := range incident.Attachments {
			if incident.Attachments[index].ID == attachmentID {
				item := incident.Attachments[index]
				found = &item
				break
			}
		}
	}
	if found == nil {
		http.NotFound(w, r)
		return
	}
	filePath := filepath.Join(s.store.dataDir, "attachments", incidentID, found.StorageName)
	w.Header().Set("Content-Type", found.MediaType)
	safeName := strings.Map(func(character rune) rune {
		if character < ' ' || character == '"' || character == '\\' {
			return -1
		}
		return character
	}, found.Name)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeName))
	http.ServeFile(w, r, filePath)
}

func (s *apiServer) backupArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	filename := fmt.Sprintf("tickets-local-complete-backup-%s.zip", time.Now().Format("2006-01-02-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	archive := zip.NewWriter(w)
	events, err := archive.Create("events.ndjson")
	if err == nil {
		err = s.store.EventLog(events)
	}
	if err == nil {
		for _, incident := range s.store.Snapshot().Incidents {
			for _, attachment := range incident.Attachments {
				source, openErr := os.Open(filepath.Join(s.store.dataDir, "attachments", incident.ID, attachment.StorageName))
				if openErr != nil {
					err = openErr
					break
				}
				entry, createErr := archive.Create(path.Join("attachments", incident.ID, attachment.StorageName))
				if createErr == nil {
					_, createErr = io.Copy(entry, io.LimitReader(source, 10<<20))
				}
				source.Close()
				if createErr != nil {
					err = createErr
					break
				}
			}
			if err != nil {
				break
			}
		}
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		s.logger.Printf("complete backup failed: %v", err)
	}
}

func (s *apiServer) restoreValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	data, ok := readRestoreUpload(w, r)
	if !ok {
		return
	}
	_, report, err := ValidateRestore(data)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *apiServer) restoreApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	data, ok := readRestoreUpload(w, r)
	if !ok {
		return
	}
	if r.FormValue("confirm") != "RESTORE" {
		writeError(w, http.StatusBadRequest, "restore confirmation is required")
		return
	}
	report, err := s.store.RestoreEventLog(data, r.FormValue("sha256"), time.Now().UTC())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func readRestoreUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreBytes+(1<<20))
	if err := r.ParseMultipartForm(maxRestoreBytes + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "restore upload is invalid or larger than 32 MB")
		return nil, false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "select an NDJSON event log")
		return nil, false
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".ndjson") {
		writeError(w, http.StatusBadRequest, "restore file must use the .ndjson extension")
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRestoreBytes+1))
	if err != nil || len(data) > maxRestoreBytes {
		writeError(w, http.StatusBadRequest, "restore file must be 32 MB or smaller")
		return nil, false
	}
	return data, true
}

func (s *apiServer) responders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input ResponderInput
	if !decodeJSON(w, r, &input) {
		return
	}
	responder, err := s.store.CreateResponder(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.aprs != nil {
		s.aprs.Notify()
	}
	writeJSON(w, http.StatusCreated, responder)
}

func (s *apiServer) responder(w http.ResponseWriter, r *http.Request) {
	pathValue := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/responders/"), "/")
	if strings.HasSuffix(pathValue, "/position") {
		s.responderPosition(w, r, strings.TrimSuffix(pathValue, "/position"))
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	id := pathValue
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	var input ResponderInput
	if !decodeJSON(w, r, &input) {
		return
	}
	responder, err := s.store.UpdateResponder(id, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.aprs != nil {
		s.aprs.Notify()
	}
	writeJSON(w, http.StatusOK, responder)
}

func (s *apiServer) responderPosition(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	var input ResponderPositionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	responder, err := s.store.UpdateResponderDevicePosition(id, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, responder)
}

func (s *apiServer) facilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input FacilityInput
	if !decodeJSON(w, r, &input) {
		return
	}
	facility, err := s.store.CreateFacility(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, facility)
}

func (s *apiServer) facility(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/facilities/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	var input FacilityInput
	if !decodeJSON(w, r, &input) {
		return
	}
	facility, err := s.store.UpdateFacility(id, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, facility)
}

func (s *apiServer) locations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input LocationInput
	if !decodeJSON(w, r, &input) {
		return
	}
	location, err := s.store.CreateLocation(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, location)
}

func (s *apiServer) location(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/locations/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input LocationInput
		if !decodeJSON(w, r, &input) {
			return
		}
		location, err := s.store.UpdateLocation(id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, location)
	case http.MethodDelete:
		if err := s.store.DeleteLocation(id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

func (s *apiServer) overlayImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxKMLBytes+(1<<20))
	if err := r.ParseMultipartForm(maxKMLBytes + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "KML upload is too large or invalid")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "choose a KML file to import")
		return
	}
	defer file.Close()
	if !strings.EqualFold(path.Ext(header.Filename), ".kml") {
		writeError(w, http.StatusBadRequest, "only .kml files are supported")
		return
	}
	input, err := parseKML(file, header.Filename)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if name := clean(r.FormValue("name")); name != "" {
		input.Name = name
	}
	if color := clean(r.FormValue("color")); color != "" {
		input.Color = color
	}
	if err := validateOverlayInput(input); err != nil {
		writeStoreError(w, err)
		return
	}
	overlay, err := s.store.CreateOverlay(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, overlay)
}

func (s *apiServer) overlay(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/overlays/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input MapOverlayUpdateInput
		if !decodeJSON(w, r, &input) {
			return
		}
		overlay, err := s.store.UpdateOverlay(id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, overlay)
	case http.MethodDelete:
		if err := s.store.DeleteOverlay(id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

func (s *apiServer) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	var input Settings
	if !decodeJSON(w, r, &input) {
		return
	}
	passcode := input.APRS.Passcode
	input.APRS.Passcode = ""
	if passcode != "" {
		if err := s.store.SaveAPRSPasscode(passcode); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	_, err := s.store.UpdateSettings(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.aprs != nil {
		s.aprs.Notify()
	}
	writeJSON(w, http.StatusOK, s.store.Snapshot().Settings)
}

func (s *apiServer) aprsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.aprs == nil {
		writeJSON(w, http.StatusOK, APRSStatus{State: "disabled", Message: "APRS receiver is unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, s.aprs.Status())
}

func (s *apiServer) aprsReconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.aprs == nil {
		writeError(w, http.StatusServiceUnavailable, "APRS receiver is unavailable")
		return
	}
	s.aprs.Notify()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "reconnecting"})
}

func (s *apiServer) aprsLocalDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.aprs == nil {
		writeError(w, http.StatusServiceUnavailable, "APRS receiver is unavailable")
		return
	}
	discovery, err := s.aprs.DiscoverLocalAudioDevices(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, discovery)
}

func (s *apiServer) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	events, unsubscribe := s.store.Subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			data, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "event: change\ndata: %s\n\n", data)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (s *apiServer) backup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	filename := fmt.Sprintf("tickets-local-backup-%s.ndjson", time.Now().Format("2006-01-02-150405"))
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	if err := s.store.EventLog(w); err != nil {
		s.logger.Printf("backup stream failed: %v", err)
	}
}

func (s *apiServer) demo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := s.store.SeedDemoData(); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

type geocodeResult struct {
	DisplayName string  `json:"display_name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Type        string  `json:"type"`
}

var intersectionSeparator = regexp.MustCompile(`(?i)\s+(?:and|at)\s+|\s*[&/@]\s*`)

func parseIntersection(query string) (string, string, string, bool) {
	parts := strings.SplitN(query, ",", 2)
	roads := intersectionSeparator.Split(strings.TrimSpace(parts[0]), 2)
	if len(roads) != 2 || strings.TrimSpace(roads[0]) == "" || strings.TrimSpace(roads[1]) == "" {
		return "", "", "", false
	}
	place := ""
	if len(parts) == 2 {
		place = strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(roads[0]), strings.TrimSpace(roads[1]), place, true
}

func overpassRoadPattern(name string) string {
	words := strings.Fields(name)
	aliases := map[string]string{
		"st": "(St|Street)", "street": "(St|Street)",
		"rd": "(Rd|Road)", "road": "(Rd|Road)",
		"ave": "(Ave|Avenue)", "avenue": "(Ave|Avenue)",
		"blvd": "(Blvd|Boulevard)", "boulevard": "(Blvd|Boulevard)",
		"hwy": "(Hwy|Highway)", "highway": "(Hwy|Highway)",
		"pkwy": "(Pkwy|Parkway)", "parkway": "(Pkwy|Parkway)",
		"ln": "(Ln|Lane)", "lane": "(Ln|Lane)",
		"dr": "(Dr|Drive)", "drive": "(Dr|Drive)",
		"ct": "(Ct|Court)", "court": "(Ct|Court)",
		"pl": "(Pl|Place)", "place": "(Pl|Place)",
		"n": "(N|North)", "north": "(N|North)",
		"s": "(S|South)", "south": "(S|South)",
		"e": "(E|East)", "east": "(E|East)",
		"w": "(W|West)", "west": "(W|West)",
	}
	for index, word := range words {
		trimmed := strings.Trim(word, ".")
		if alias, ok := aliases[strings.ToLower(trimmed)]; ok {
			words[index] = alias
		} else {
			words[index] = regexp.QuoteMeta(word)
		}
	}
	return "^" + strings.Join(words, `[[:space:]]+`) + "$"
}

func overpassString(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func (s *apiServer) intersectionResults(ctx context.Context, road1, road2, place string, settings Settings) ([]geocodeResult, error) {
	query := fmt.Sprintf(`[out:json][timeout:15];way(around:160934,%.6f,%.6f)["highway"]["name"~"%s",i]->.a;way(around:160934,%.6f,%.6f)["highway"]["name"~"%s",i]->.b;node(w.a)(w.b);out body;`,
		settings.CenterLat, settings.CenterLon, overpassString(overpassRoadPattern(road1)),
		settings.CenterLat, settings.CenterLon, overpassString(overpassRoadPattern(road2)))
	form := url.Values{"data": {query}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.overpassURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Elements []struct {
			ID  int64   `json:"id"`
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"elements"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]geocodeResult, 0, len(payload.Elements))
	label := road1 + " & " + road2
	if place != "" {
		label += ", " + place
	}
	for _, element := range payload.Elements {
		if element.Lat >= -90 && element.Lat <= 90 && element.Lon >= -180 && element.Lon <= 180 {
			results = append(results, geocodeResult{DisplayName: label, Latitude: element.Lat, Longitude: element.Lon, Type: "intersection"})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		di := (results[i].Latitude-settings.CenterLat)*(results[i].Latitude-settings.CenterLat) + (results[i].Longitude-settings.CenterLon)*(results[i].Longitude-settings.CenterLon)
		dj := (results[j].Latitude-settings.CenterLat)*(results[j].Latitude-settings.CenterLat) + (results[j].Longitude-settings.CenterLon)*(results[j].Longitude-settings.CenterLon)
		return di < dj
	})
	if len(results) > 5 {
		results = results[:5]
	}
	return results, nil
}

func (s *apiServer) geocode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	query := clean(r.URL.Query().Get("q"))
	if len(query) < 3 {
		writeError(w, http.StatusBadRequest, "enter a more complete location")
		return
	}
	if len(query) > 300 {
		writeError(w, http.StatusBadRequest, "location is too long")
		return
	}

	s.geocodeMu.Lock()
	wait := time.Until(s.lastGeocode.Add(time.Second))
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-r.Context().Done():
			timer.Stop()
			s.geocodeMu.Unlock()
			return
		case <-timer.C:
		}
	}
	s.lastGeocode = time.Now()
	s.geocodeMu.Unlock()

	endpoint, err := url.Parse(s.geocodeURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "location search is not configured correctly")
		return
	}
	parameters := endpoint.Query()
	parameters.Set("format", "jsonv2")
	parameters.Set("addressdetails", "1")
	parameters.Set("dedupe", "1")
	parameters.Set("limit", "5")
	parameters.Set("q", query)
	settings := s.store.Snapshot().Settings
	const bias = 2.5
	parameters.Set("viewbox", fmt.Sprintf(
		"%.6f,%.6f,%.6f,%.6f",
		settings.CenterLon-bias,
		settings.CenterLat+bias,
		settings.CenterLon+bias,
		settings.CenterLat-bias,
	))
	parameters.Set("bounded", "0")
	endpoint.RawQuery = parameters.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not prepare location search")
		return
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	request.Header.Set("Accept", "application/json")
	language := clean(r.Header.Get("Accept-Language"))
	if language == "" {
		language = "en"
	}
	if len(language) > 120 {
		language = language[:120]
	}
	request.Header.Set("Accept-Language", language)
	response, err := s.client.Do(request)
	if err != nil {
		s.logger.Printf("location search provider unavailable: %v", err)
		writeError(w, http.StatusBadGateway, "location search is unavailable; coordinates can be entered manually")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		s.logger.Printf("location search provider returned HTTP %d", response.StatusCode)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("location provider returned HTTP %d; try again or enter coordinates manually", response.StatusCode))
		return
	}
	var raw []struct {
		DisplayName string `json:"display_name"`
		Latitude    string `json:"lat"`
		Longitude   string `json:"lon"`
		Type        string `json:"type"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&raw); err != nil {
		writeError(w, http.StatusBadGateway, "location provider returned an unreadable response")
		return
	}
	results := make([]geocodeResult, 0, len(raw))
	for _, item := range raw {
		lat, latErr := strconv.ParseFloat(item.Latitude, 64)
		lon, lonErr := strconv.ParseFloat(item.Longitude, 64)
		if latErr == nil && lonErr == nil {
			results = append(results, geocodeResult{DisplayName: item.DisplayName, Latitude: lat, Longitude: lon, Type: item.Type})
		}
	}
	if len(results) == 0 {
		if road1, road2, place, ok := parseIntersection(query); ok {
			fallback, fallbackErr := s.intersectionResults(r.Context(), road1, road2, place, settings)
			if fallbackErr != nil {
				s.logger.Printf("cross-street search provider unavailable: %v", fallbackErr)
			} else {
				results = fallback
			}
		}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *apiServer) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "API endpoint not found")
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.web, name)
	if err != nil {
		data, err = fs.ReadFile(s.web, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	// Tickets Local is a tiny embedded local application. Avoid leaving an
	// operator on an older JavaScript or stylesheet after replacing the binary.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *apiServer) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// Radar responses are converted to object URLs in the browser. Allow the
		// resulting blob: image while keeping every other resource local or on the
		// explicitly approved OpenStreetMap tile host.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob: https://tile.openstreetmap.org; connect-src 'self'; style-src 'self'; script-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *apiServer) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Printf("request panic: %v", recovered)
				writeError(w, http.StatusInternalServerError, "an unexpected error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *apiServer) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/events") && r.URL.Path != "/healthz" {
			s.logger.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+friendlyJSONError(err))
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request: only one JSON object is allowed")
		return false
	}
	return true
}

func friendlyJSONError(err error) string {
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxError):
		return "malformed JSON"
	case errors.As(err, &typeError):
		return fmt.Sprintf("field %s has the wrong value type", typeError.Field)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return strings.Replace(err.Error(), "json: unknown field ", "unknown field ", 1)
	case errors.Is(err, io.EOF):
		return "request body is empty"
	default:
		return err.Error()
	}
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "record not found")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "this record changed on another computer; review the latest version and try again")
	default:
		var validation validationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusBadRequest, validation.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "could not save the change")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func splitPath(value string) []string {
	raw := strings.Split(strings.Trim(value, "/"), "/")
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func shutdownServer(ctx context.Context, server *http.Server) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
