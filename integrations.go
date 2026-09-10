package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxIntegrationResponse = 5 << 20

var osmRepeaterEndpoint = "https://overpass-api.de/api/interpreter"

type IntegrationService struct {
	mu        sync.Mutex
	store     *Store
	client    *http.Client
	logger    *log.Logger
	cachePath string
	cached    IntegrationStatus
	cacheKey  string
}

func NewIntegrationService(store *Store, client *http.Client, logger *log.Logger) *IntegrationService {
	s := &IntegrationService{store: store, client: client, logger: logger, cachePath: filepath.Join(store.dataDir, "integration-cache.json"), cached: emptyIntegrationStatus("waiting", "Integration feeds have not updated yet")}
	s.loadCache()
	return s
}

func emptyIntegrationStatus(state, message string) IntegrationStatus {
	return IntegrationStatus{State: state, Message: message, StormReports: []ExternalMapFeature{}, Infrastructure: []ExternalMapFeature{}, AmateurRepeaters: []ExternalMapFeature{}, GMRSRepeaters: []ExternalMapFeature{}, MeshCoreNodes: []ExternalMapFeature{}, AREDNNodes: []AREDNNodeStatus{}, Sensors: []SensorObservation{}}
}

func normalizeEndpointList(input string, limit int, label string) ([]string, error) {
	result := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Fields(input) {
		value := strings.TrimSpace(strings.TrimSuffix(raw, ","))
		if value == "" {
			continue
		}
		if err := validateWeatherProviderURL(value, label); err != nil {
			return nil, err
		}
		parsed, _ := url.Parse(value)
		if parsed.RawQuery != "" {
			return nil, validationError{label + " endpoint URLs may not contain query credentials or parameters"}
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if len(result) > limit {
		return nil, validationError{fmt.Sprintf("configure no more than %d %s endpoints", limit, label)}
	}
	return result, nil
}

func (s *IntegrationService) Status(ctx context.Context) IntegrationStatus {
	settings := s.store.Snapshot().Settings.Integrations
	if !settings.Enabled {
		return emptyIntegrationStatus("disabled", "External operational feeds are off")
	}
	key := fmt.Sprintf("%+v", settings)
	s.mu.Lock()
	defer s.mu.Unlock()
	refresh := time.Duration(settings.RefreshMinutes) * time.Minute
	if s.cacheKey == key && s.cached.UpdatedAt != nil && time.Since(*s.cached.UpdatedAt) < refresh {
		return s.cached
	}
	status := emptyIntegrationStatus("current", "")
	failures := []string{}
	if settings.StormReportsURL != "" {
		items, err := s.fetchGeoJSON(ctx, settings.StormReportsURL, "storm", 250)
		if err != nil {
			failures = append(failures, "storm reports")
		} else {
			status.StormReports = items
		}
	}
	if settings.InfrastructureURL != "" {
		items, err := s.fetchGeoJSON(ctx, settings.InfrastructureURL, "infrastructure", 500)
		if err != nil {
			failures = append(failures, "infrastructure")
		} else {
			status.Infrastructure = items
		}
	}
	if settings.AmateurRepeatersURL != "" {
		items, err := s.fetchGeoJSON(ctx, settings.AmateurRepeatersURL, "amateur_repeater", 1000)
		if err != nil {
			failures = append(failures, "amateur repeaters")
		} else {
			status.AmateurRepeaters = items
		}
	} else if settings.AmateurOSMEnabled {
		home := s.store.Snapshot().Settings
		items, err := s.fetchOSMRepeaters(ctx, home.CenterLat, home.CenterLon)
		if err != nil {
			failures = append(failures, "OpenStreetMap amateur repeaters")
		} else {
			status.AmateurRepeaters = items
		}
	}
	if settings.GMRSRepeatersURL != "" {
		items, err := s.fetchGeoJSON(ctx, settings.GMRSRepeatersURL, "gmrs_repeater", 1000)
		if err != nil {
			failures = append(failures, "GMRS repeaters")
		} else {
			status.GMRSRepeaters = items
		}
	}
	if settings.MeshCoreURL != "" {
		items, err := s.fetchMeshCore(ctx, settings.MeshCoreURL)
		if err != nil {
			failures = append(failures, "MeshCore nodes")
		} else {
			status.MeshCoreNodes = items
		}
	}
	nodes, _ := normalizeEndpointList(settings.AREDNNodeURLs, 10, "AREDN node")
	for _, endpoint := range nodes {
		status.AREDNNodes = append(status.AREDNNodes, s.fetchAREDN(ctx, endpoint))
	}
	sensors, _ := normalizeEndpointList(settings.SensorURLs, 10, "sensor")
	for _, endpoint := range sensors {
		items, err := s.fetchSensors(ctx, endpoint)
		if err != nil {
			failures = append(failures, "sensor "+hostLabel(endpoint))
		} else {
			status.Sensors = append(status.Sensors, items...)
		}
	}
	now := time.Now().UTC()
	status.UpdatedAt = &now
	status.Message = fmt.Sprintf("%d storm report(s), %d infrastructure item(s), %d amateur repeater(s), %d GMRS repeater(s), %d MeshCore node(s), %d AREDN node(s), %d sensor observation(s)", len(status.StormReports), len(status.Infrastructure), len(status.AmateurRepeaters), len(status.GMRSRepeaters), len(status.MeshCoreNodes), len(status.AREDNNodes), len(status.Sensors))
	if len(failures) > 0 {
		status.State = "partial"
		status.Message += " · unavailable: " + strings.Join(failures, ", ")
	}
	if len(failures) > 0 && len(status.StormReports)+len(status.Infrastructure)+len(status.AmateurRepeaters)+len(status.GMRSRepeaters)+len(status.MeshCoreNodes)+len(status.AREDNNodes)+len(status.Sensors) == 0 && s.cached.UpdatedAt != nil {
		stale := s.cached
		stale.State = "stale"
		stale.Stale = true
		stale.Message = "Using cached integration data; sources unavailable"
		return stale
	}
	s.cached = status
	s.cacheKey = key
	if err := s.saveCache(status); err != nil {
		s.logger.Printf("integration cache: %v", err)
	}
	return status
}

type meshCoreEnvelope struct {
	Version int `json:"version"`
	Nodes   []struct {
		ID             string   `json:"id"`
		Name           string   `json:"name"`
		Type           string   `json:"type"`
		Latitude       float64  `json:"latitude"`
		Longitude      float64  `json:"longitude"`
		LastSeenAt     string   `json:"last_seen_at"`
		SNR            *float64 `json:"snr"`
		BatteryPercent *float64 `json:"battery_percent"`
		Details        string   `json:"details"`
	} `json:"nodes"`
}

func (s *IntegrationService) fetchMeshCore(ctx context.Context, endpoint string) ([]ExternalMapFeature, error) {
	var payload meshCoreEnvelope
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	if payload.Version != 1 {
		return nil, errors.New("unsupported MeshCore format version")
	}
	if len(payload.Nodes) > 1000 {
		return nil, errors.New("MeshCore response exceeds 1000 nodes")
	}
	result := []ExternalMapFeature{}
	for _, node := range payload.Nodes {
		id, name := abbreviate(clean(node.ID), 160), abbreviate(clean(node.Name), 200)
		if id == "" || name == "" || !validCoordinates(&node.Latitude, &node.Longitude) || !bounded(node.BatteryPercent, 0, 100) || !bounded(node.SNR, -200, 100) {
			continue
		}
		details := []string{}
		if node.SNR != nil {
			details = append(details, fmt.Sprintf("SNR %.1f dB", *node.SNR))
		}
		if node.BatteryPercent != nil {
			details = append(details, fmt.Sprintf("Battery %.0f%%", *node.BatteryPercent))
		}
		if text := abbreviate(clean(node.Details), 800); text != "" {
			details = append(details, text)
		}
		result = append(result, ExternalMapFeature{ID: id, Name: name, Kind: abbreviate(clean(node.Type), 100), Source: hostLabel(endpoint), ObservedAt: parseExternalTime(node.LastSeenAt), Latitude: node.Latitude, Longitude: node.Longitude, Details: abbreviate(strings.Join(details, " · "), 1000)})
	}
	return result, nil
}

type geoJSONFeed struct {
	Features []struct {
		ID       any `json:"id"`
		Geometry struct {
			Type        string    `json:"type"`
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties map[string]any `json:"properties"`
	} `json:"features"`
}

type overpassRepeaterFeed struct {
	Elements []struct {
		Type   string  `json:"type"`
		ID     int64   `json:"id"`
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
		Center *struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"center"`
		Tags map[string]string `json:"tags"`
	} `json:"elements"`
}

func (s *IntegrationService) fetchOSMRepeaters(ctx context.Context, latitude, longitude float64) ([]ExternalMapFeature, error) {
	query := fmt.Sprintf(`[out:json][timeout:15];nwr(around:160934,%.6f,%.6f)["communication:amateur_radio:repeater"];out center tags 1000;`, latitude, longitude)
	var feed overpassRepeaterFeed
	if err := s.getJSON(ctx, osmRepeaterEndpoint+"?data="+url.QueryEscape(query), &feed); err != nil {
		return nil, err
	}
	result := []ExternalMapFeature{}
	for _, element := range feed.Elements {
		lat, lon := element.Lat, element.Lon
		if element.Center != nil {
			lat, lon = element.Center.Lat, element.Center.Lon
		}
		if (lat == 0 && lon == 0) || !validCoordinates(&lat, &lon) {
			continue
		}
		tags := element.Tags
		properties := map[string]any{
			"frequency": tags["communication:amateur_radio:repeater:frequency_out"],
			"offset":    tags["communication:amateur_radio:repeater:shift"],
			"tone":      tags["communication:amateur_radio:repeater:ctcss"],
			"mode":      tags["communication:amateur_radio:repeater:modulation"],
			"details":   tags["description"],
		}
		name := defaultString(tags["communication:amateur_radio:callsign"], defaultString(tags["communication:amateur_radio:repeater"], tags["name"]))
		result = append(result, ExternalMapFeature{ID: fmt.Sprintf("osm-%s-%d", element.Type, element.ID), Name: abbreviate(clean(name), 200), Kind: abbreviate(clean(tags["communication:amateur_radio:repeater:modulation"]), 100), Source: "OpenStreetMap contributors", Latitude: lat, Longitude: lon, Details: abbreviate(repeaterFeatureDetails(properties, ""), 1000)})
	}
	return result, nil
}

func (s *IntegrationService) fetchGeoJSON(ctx context.Context, endpoint, kind string, limit int) ([]ExternalMapFeature, error) {
	var feed geoJSONFeed
	if err := s.getJSON(ctx, endpoint, &feed); err != nil {
		return nil, err
	}
	result := []ExternalMapFeature{}
	for index, feature := range feed.Features {
		if len(result) >= limit {
			break
		}
		if !strings.EqualFold(feature.Geometry.Type, "Point") || len(feature.Geometry.Coordinates) < 2 {
			continue
		}
		lon, lat := feature.Geometry.Coordinates[0], feature.Geometry.Coordinates[1]
		if !validCoordinates(&lat, &lon) {
			continue
		}
		props := feature.Properties
		observed := parseExternalTime(firstString(props, "observed_at", "observed", "timestamp", "time", "valid"))
		id := fmt.Sprint(feature.ID)
		if id == "<nil>" || id == "" {
			id = fmt.Sprintf("%s-%d", kind, index)
		}
		details := firstString(props, "details", "description", "remarks", "headline")
		if kind == "amateur_repeater" || kind == "gmrs_repeater" {
			details = repeaterFeatureDetails(props, details)
		}
		result = append(result, ExternalMapFeature{ID: abbreviate(id, 160), Name: abbreviate(firstString(props, "callsign", "name", "title", "location"), 200), Kind: abbreviate(firstString(props, "mode", "kind", "type", "status"), 100), Source: abbreviate(firstString(props, "source", "database", "coordinator"), 160), ObservedAt: observed, Latitude: lat, Longitude: lon, Details: abbreviate(details, 1000)})
	}
	return result, nil
}

func repeaterFeatureDetails(properties map[string]any, fallback string) string {
	parts := []string{}
	for _, field := range []struct {
		label string
		keys  []string
	}{
		{"Frequency", []string{"frequency", "output_frequency", "frequency_out", "rx_frequency"}},
		{"Offset", []string{"offset", "shift"}},
		{"Tone", []string{"tone", "ctcss", "pl", "dcs"}},
		{"Mode", []string{"mode", "modulation"}},
		{"Use", []string{"use", "access", "status"}},
	} {
		if value := firstString(properties, field.keys...); value != "" {
			parts = append(parts, field.label+" "+value)
		}
	}
	if fallback != "" {
		parts = append(parts, fallback)
	}
	return strings.Join(parts, " · ")
}

type sensorEnvelope struct {
	Version      int                 `json:"version"`
	Observations []SensorObservation `json:"observations"`
}

func (s *IntegrationService) fetchSensors(ctx context.Context, endpoint string) ([]SensorObservation, error) {
	var payload sensorEnvelope
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	if payload.Version != 1 {
		return nil, errors.New("unsupported sensor format version")
	}
	if len(payload.Observations) > 100 {
		return nil, errors.New("sensor response exceeds 100 observations")
	}
	result := []SensorObservation{}
	for _, item := range payload.Observations {
		item.ID = abbreviate(clean(item.ID), 100)
		item.Name = abbreviate(clean(item.Name), 160)
		item.Status = abbreviate(clean(item.Status), 100)
		item.Source = hostLabel(endpoint)
		if item.ID == "" || item.ObservedAt.IsZero() {
			continue
		}
		if item.Latitude != nil && item.Longitude != nil && !validCoordinates(item.Latitude, item.Longitude) {
			continue
		}
		if !bounded(item.TemperatureF, -100, 180) || !bounded(item.HumidityPercent, 0, 100) || !bounded(item.BatteryPercent, 0, 100) || !bounded(item.WindSpeedMPH, 0, 300) || !bounded(item.WaterDepthFeet, -100, 1000) {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}
func bounded(value *float64, min, max float64) bool {
	return value == nil || (*value >= min && *value <= max)
}

func (s *IntegrationService) fetchAREDN(ctx context.Context, endpoint string) AREDNNodeStatus {
	started := time.Now()
	checked := started.UTC()
	result := AREDNNodeStatus{URL: endpoint, Name: hostLabel(endpoint), CheckedAt: checked}
	var payload map[string]any
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		result.Message = abbreviate(err.Error(), 240)
		return result
	}
	result.Reachable = true
	result.LatencyMS = time.Since(started).Milliseconds()
	result.Name = defaultString(firstString(payload, "name", "node", "hostname", "node_name"), result.Name)
	if value, ok := firstNumber(payload, "route_cost", "cost", "metric", "etx"); ok {
		result.RouteCost = &value
	}
	result.Message = defaultString(firstString(payload, "status", "message"), "Reachable")
	return result
}

func (s *IntegrationService) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version)
	request.Header.Set("Accept", "application/json, application/geo+json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxIntegrationResponse+1))
	if err = decoder.Decode(target); err != nil {
		return errors.New("invalid JSON response")
	}
	return nil
}
func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if text, ok := value.(string); ok {
				return clean(text)
			}
		}
	}
	return ""
}
func firstNumber(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		switch value := values[key].(type) {
		case float64:
			return value, true
		case string:
			number, err := strconv.ParseFloat(value, 64)
			return number, err == nil
		}
	}
	return 0, false
}
func parseExternalTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}
func hostLabel(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	return parsed.Hostname()
}
func (s *IntegrationService) loadCache() {
	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		return
	}
	if json.Unmarshal(data, &s.cached) == nil && s.cached.UpdatedAt != nil {
		s.cached.State = "stale"
		s.cached.Stale = true
	}
}
func (s *IntegrationService) saveCache(value IntegrationStatus) error {
	file, err := os.CreateTemp(filepath.Dir(s.cachePath), ".integration-*.json")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Chmod(0600); err == nil {
		err = json.NewEncoder(file).Encode(value)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temp, s.cachePath)
}
