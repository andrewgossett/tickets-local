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
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultNWSAlertsURL = "https://api.weather.gov/alerts/active"
	maxWeatherResponse  = 8 << 20
	maxWeatherAlerts    = 100
	maxAlertCoordinates = 10000
)

type WeatherService struct {
	mu         sync.Mutex
	store      *Store
	client     *http.Client
	logger     *log.Logger
	endpoint   string
	nwsAPIBase string
	cachePath  string
	cached     WeatherStatus
	radarCache map[string]radarCacheEntry
}

func NewWeatherService(store *Store, client *http.Client, logger *log.Logger) *WeatherService {
	endpoint := strings.TrimSpace(os.Getenv("TICKETS_LOCAL_NWS_ALERTS_URL"))
	if endpoint == "" {
		endpoint = defaultNWSAlertsURL
	}
	nwsAPIBase := strings.TrimRight(strings.TrimSpace(os.Getenv("TICKETS_LOCAL_NWS_API_URL")), "/")
	if nwsAPIBase == "" {
		nwsAPIBase = "https://api.weather.gov"
	}
	service := &WeatherService{
		store:      store,
		client:     client,
		logger:     logger,
		endpoint:   endpoint,
		nwsAPIBase: nwsAPIBase,
		cachePath:  filepath.Join(store.dataDir, "weather-cache.json"),
		cached:     WeatherStatus{State: "waiting", Message: "Weather has not updated yet", Source: "National Weather Service", Alerts: []WeatherAlert{}},
		radarCache: make(map[string]radarCacheEntry),
	}
	service.loadCache()
	return service
}

func (service *WeatherService) Status(ctx context.Context) WeatherStatus {
	settings := service.store.Snapshot().Settings
	if !settings.Weather.Enabled {
		return WeatherStatus{State: "disabled", Message: "Weather awareness is off", Source: "National Weather Service", Alerts: []WeatherAlert{}}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	refresh := time.Duration(settings.Weather.RefreshMinutes) * time.Minute
	conditionsExpected := settings.Weather.AlertsURL == "" && (service.endpoint == defaultNWSAlertsURL || os.Getenv("TICKETS_LOCAL_NWS_API_URL") != "")
	if service.cached.UpdatedAt != nil && (!conditionsExpected || service.cached.Current != nil) && time.Since(*service.cached.UpdatedAt) < refresh && sameWeatherCenter(service.cached, settings) {
		return service.cached
	}
	status, err := service.fetch(ctx, settings)
	if err == nil {
		service.cached = status
		if cacheErr := service.saveCache(status); cacheErr != nil {
			service.logger.Printf("weather cache: %v", cacheErr)
		}
		return status
	}
	service.logger.Printf("weather update: %v", err)
	if service.cached.UpdatedAt != nil && sameWeatherCenter(service.cached, settings) {
		stale := service.cached
		stale.State = "stale"
		stale.Stale = true
		stale.Message = fmt.Sprintf("Using cached weather; update failed: %v", err)
		return stale
	}
	return WeatherStatus{
		State: "unavailable", Message: fmt.Sprintf("Weather is unavailable: %v", err),
		Source: "National Weather Service", CenterLat: settings.CenterLat, CenterLon: settings.CenterLon,
		Alerts: []WeatherAlert{},
	}
}

func (service *WeatherService) fetch(ctx context.Context, settings Settings) (WeatherStatus, error) {
	endpointText := settings.Weather.AlertsURL
	if endpointText == "" {
		endpointText = service.endpoint
	}
	endpoint, err := url.Parse(endpointText)
	if err != nil || !oneOf(endpoint.Scheme, "http", "https") || endpoint.Host == "" {
		return WeatherStatus{}, errors.New("weather provider address is invalid")
	}
	query := endpoint.Query()
	query.Set("point", fmt.Sprintf("%.4f,%.4f", settings.CenterLat, settings.CenterLon))
	query.Set("status", "actual")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return WeatherStatus{}, errors.New("could not prepare weather request")
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	request.Header.Set("Accept", "application/geo+json, application/json")
	response, err := service.client.Do(request)
	if err != nil {
		return WeatherStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return WeatherStatus{}, fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	var payload nwsAlertCollection
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxWeatherResponse))
	if err := decoder.Decode(&payload); err != nil {
		return WeatherStatus{}, errors.New("provider returned an invalid alert response")
	}
	alerts := make([]WeatherAlert, 0, min(len(payload.Features), maxWeatherAlerts))
	for _, feature := range payload.Features {
		if len(alerts) >= maxWeatherAlerts {
			break
		}
		alert := normalizeNWSAlert(feature)
		if alert.ID == "" || alert.Event == "" {
			continue
		}
		alerts = append(alerts, alert)
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		return weatherSeverityRank(alerts[i].Severity) < weatherSeverityRank(alerts[j].Severity)
	})
	now := time.Now().UTC()
	message := "No active NWS alerts at the configured map home"
	if len(alerts) > 0 {
		message = fmt.Sprintf("%d active NWS alert(s) at the configured map home", len(alerts))
	}
	source := "National Weather Service"
	if settings.Weather.AlertsURL != "" {
		source = "Configured weather source (" + endpoint.Hostname() + ")"
	}
	current := payload.Current
	if settings.Weather.AlertsURL == "" && (service.endpoint == defaultNWSAlertsURL || os.Getenv("TICKETS_LOCAL_NWS_API_URL") != "") {
		if observation, observationErr := service.fetchCurrentConditions(ctx, settings.CenterLat, settings.CenterLon); observationErr == nil {
			current = observation
		} else {
			service.logger.Printf("current weather: %v", observationErr)
		}
	}
	return WeatherStatus{State: "current", Message: message, UpdatedAt: &now, Source: source, CenterLat: settings.CenterLat, CenterLon: settings.CenterLon, Alerts: alerts, Current: current}, nil
}

type nwsAlertCollection struct {
	Features []nwsAlertFeature `json:"features"`
	Current  *CurrentWeather   `json:"current,omitempty"`
}

func (service *WeatherService) fetchCurrentConditions(ctx context.Context, latitude, longitude float64) (*CurrentWeather, error) {
	var point struct {
		Properties struct {
			ObservationStations string `json:"observationStations"`
		} `json:"properties"`
	}
	if err := service.fetchNWSJSON(ctx, fmt.Sprintf("%s/points/%.4f,%.4f", service.nwsAPIBase, latitude, longitude), &point); err != nil {
		return nil, err
	}
	if point.Properties.ObservationStations == "" {
		return nil, errors.New("NWS did not return observation stations")
	}
	var stations struct {
		Features []struct {
			ID string `json:"id"`
		} `json:"features"`
	}
	if err := service.fetchNWSJSON(ctx, point.Properties.ObservationStations, &stations); err != nil {
		return nil, err
	}
	if len(stations.Features) == 0 || stations.Features[0].ID == "" {
		return nil, errors.New("NWS did not return a nearby station")
	}
	stationURL := stations.Features[0].ID
	var observation struct {
		Properties struct {
			TextDescription string     `json:"textDescription"`
			Timestamp       *time.Time `json:"timestamp"`
			Temperature     struct {
				Value *float64 `json:"value"`
			} `json:"temperature"`
			HeatIndex struct {
				Value *float64 `json:"value"`
			} `json:"heatIndex"`
			WindChill struct {
				Value *float64 `json:"value"`
			} `json:"windChill"`
			RelativeHumidity struct {
				Value *float64 `json:"value"`
			} `json:"relativeHumidity"`
			WindSpeed struct {
				Value *float64 `json:"value"`
			} `json:"windSpeed"`
			WindDirection struct {
				Value *float64 `json:"value"`
			} `json:"windDirection"`
			BarometricPressure struct {
				Value *float64 `json:"value"`
			} `json:"barometricPressure"`
			Visibility struct {
				Value *float64 `json:"value"`
			} `json:"visibility"`
		} `json:"properties"`
	}
	if err := service.fetchNWSJSON(ctx, stationURL+"/observations/latest", &observation); err != nil {
		return nil, err
	}
	p := observation.Properties
	result := &CurrentWeather{Station: path.Base(stationURL), Description: abbreviate(clean(p.TextDescription), 160), ObservedAt: p.Timestamp, HumidityPercent: p.RelativeHumidity.Value, WindDirectionDegrees: p.WindDirection.Value}
	result.TemperatureF = celsiusToFahrenheit(p.Temperature.Value)
	result.FeelsLikeF = celsiusToFahrenheit(p.HeatIndex.Value)
	if result.FeelsLikeF == nil {
		result.FeelsLikeF = celsiusToFahrenheit(p.WindChill.Value)
	}
	result.WindSpeedMPH = scaleWeatherValue(p.WindSpeed.Value, .621371)
	result.BarometerInHg = scaleWeatherValue(p.BarometricPressure.Value, .0002953)
	result.VisibilityMiles = scaleWeatherValue(p.Visibility.Value, .000621371)
	return result, nil
}

func (service *WeatherService) fetchNWSJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	request.Header.Set("Accept", "application/geo+json, application/json")
	response, err := service.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("NWS returned HTTP %d", response.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(response.Body, maxWeatherResponse)).Decode(target) != nil {
		return errors.New("NWS returned invalid observation data")
	}
	return nil
}

func celsiusToFahrenheit(value *float64) *float64 {
	if value == nil {
		return nil
	}
	converted := *value*9/5 + 32
	return &converted
}
func scaleWeatherValue(value *float64, factor float64) *float64 {
	if value == nil {
		return nil
	}
	converted := *value * factor
	return &converted
}

type nwsAlertFeature struct {
	ID       string `json:"id"`
	Geometry struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Event       string     `json:"event"`
		Headline    string     `json:"headline"`
		Severity    string     `json:"severity"`
		Urgency     string     `json:"urgency"`
		Certainty   string     `json:"certainty"`
		Description string     `json:"description"`
		Instruction string     `json:"instruction"`
		AreaDesc    string     `json:"areaDesc"`
		Effective   *time.Time `json:"effective"`
		Expires     *time.Time `json:"expires"`
	} `json:"properties"`
}

func normalizeNWSAlert(feature nwsAlertFeature) WeatherAlert {
	return WeatherAlert{
		ID: abbreviate(clean(feature.ID), 300), Event: abbreviate(clean(feature.Properties.Event), 160),
		Headline: abbreviate(clean(feature.Properties.Headline), 500), Severity: strings.ToLower(clean(feature.Properties.Severity)),
		Urgency: strings.ToLower(clean(feature.Properties.Urgency)), Certainty: strings.ToLower(clean(feature.Properties.Certainty)),
		Description: abbreviate(clean(feature.Properties.Description), 12000), Instruction: abbreviate(clean(feature.Properties.Instruction), 12000),
		Area: abbreviate(clean(feature.Properties.AreaDesc), 1000), Effective: feature.Properties.Effective, Expires: feature.Properties.Expires,
		Paths: decodeWeatherPaths(feature.Geometry.Type, feature.Geometry.Coordinates),
	}
}

func decodeWeatherPaths(kind string, raw json.RawMessage) [][]MapCoordinate {
	var polygons [][][][]float64
	switch strings.ToLower(kind) {
	case "polygon":
		var polygon [][][]float64
		if json.Unmarshal(raw, &polygon) != nil {
			return [][]MapCoordinate{}
		}
		polygons = [][][][]float64{polygon}
	case "multipolygon":
		if json.Unmarshal(raw, &polygons) != nil {
			return [][]MapCoordinate{}
		}
	default:
		return [][]MapCoordinate{}
	}
	paths := make([][]MapCoordinate, 0)
	count := 0
	for _, polygon := range polygons {
		for _, ring := range polygon {
			path := make([]MapCoordinate, 0, len(ring))
			for _, coordinate := range ring {
				if len(coordinate) < 2 || count >= maxAlertCoordinates {
					break
				}
				lon, lat := coordinate[0], coordinate[1]
				if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
					continue
				}
				path = append(path, MapCoordinate{Latitude: lat, Longitude: lon})
				count++
			}
			if len(path) >= 3 {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func weatherSeverityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "extreme":
		return 0
	case "severe":
		return 1
	case "moderate":
		return 2
	case "minor":
		return 3
	default:
		return 4
	}
}

func sameWeatherCenter(status WeatherStatus, settings Settings) bool {
	return status.CenterLat == settings.CenterLat && status.CenterLon == settings.CenterLon
}

func (service *WeatherService) loadCache() {
	file, err := os.Open(service.cachePath)
	if err != nil {
		return
	}
	defer file.Close()
	var status WeatherStatus
	if json.NewDecoder(io.LimitReader(file, maxWeatherResponse)).Decode(&status) == nil && status.UpdatedAt != nil {
		status.Stale = true
		status.State = "stale"
		status.Message = "Using weather saved from the previous session"
		if status.Alerts == nil {
			status.Alerts = []WeatherAlert{}
		}
		service.cached = status
	}
}

func (service *WeatherService) saveCache(status WeatherStatus) error {
	file, err := os.CreateTemp(filepath.Dir(service.cachePath), ".weather-*.json")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(status); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, service.cachePath)
}
