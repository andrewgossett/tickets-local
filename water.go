package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultUSGSWaterURL = "https://waterservices.usgs.gov/nwis/iv/"
const defaultUSGSSiteURL = "https://waterservices.usgs.gov/nwis/site/"
const defaultNOAAWaterURL = "https://api.water.noaa.gov/nwps/v1/gauges"
const maxWaterResponse = 8 << 20

var waterSitePattern = regexp.MustCompile(`^[0-9A-Za-z-]{1,20}$`)

type WaterService struct {
	mu        sync.Mutex
	store     *Store
	client    *http.Client
	logger    *log.Logger
	cachePath string
	cached    WaterStatus
	cacheKey  string
	siteURL   string
}

func NewWaterService(store *Store, client *http.Client, logger *log.Logger) *WaterService {
	s := &WaterService{store: store, client: client, logger: logger, cachePath: filepath.Join(store.dataDir, "water-cache.json"), siteURL: defaultUSGSSiteURL, cached: WaterStatus{State: "waiting", Message: "Water gauges have not updated yet", Source: "USGS Water Services", Gauges: []WaterGauge{}}}
	s.loadCache()
	return s
}

func (s *WaterService) DiscoverSites(ctx context.Context, latitude, longitude, radiusMiles float64) ([]WaterSiteCandidate, error) {
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return nil, validationError{"map center coordinates are invalid"}
	}
	if radiusMiles < 5 || radiusMiles > 200 {
		return nil, validationError{"gauge search radius must be between 5 and 200 miles"}
	}
	latitudeDelta := radiusMiles / 69.0
	longitudeScale := math.Max(.2, math.Cos(latitude*math.Pi/180))
	longitudeDelta := radiusMiles / (69.0 * longitudeScale)
	endpoint, err := url.Parse(s.siteURL)
	if err != nil || !oneOf(endpoint.Scheme, "http", "https") || endpoint.Host == "" {
		return nil, errors.New("USGS gauge directory address is invalid")
	}
	query := endpoint.Query()
	query.Set("format", "rdb")
	query.Set("bBox", fmt.Sprintf("%.5f,%.5f,%.5f,%.5f", longitude-longitudeDelta, latitude-latitudeDelta, longitude+longitudeDelta, latitude+latitudeDelta))
	query.Set("siteStatus", "active")
	query.Set("siteType", "ST")
	query.Set("hasDataTypeCd", "iv")
	query.Set("siteOutput", "basic")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("could not prepare USGS gauge search")
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("USGS gauge search failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("USGS gauge search returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxWaterResponse+1))
	if err != nil || len(body) > maxWaterResponse {
		return nil, errors.New("USGS gauge directory returned invalid data")
	}
	return parseUSGSSites(string(body), latitude, longitude, radiusMiles), nil
}

func parseUSGSSites(body string, latitude, longitude, radiusMiles float64) []WaterSiteCandidate {
	var columns []string
	results := []WaterSiteCandidate{}
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(rawLine, "\t")
		if columns == nil {
			columns = fields
			continue
		}
		if len(fields) != len(columns) || (len(fields) > 1 && strings.HasSuffix(fields[0], "s") && strings.HasSuffix(fields[1], "s")) {
			continue
		}
		values := map[string]string{}
		for index, column := range columns {
			values[column] = strings.TrimSpace(fields[index])
		}
		lat, latErr := strconv.ParseFloat(values["dec_lat_va"], 64)
		lon, lonErr := strconv.ParseFloat(values["dec_long_va"], 64)
		id := clean(values["site_no"])
		if latErr != nil || lonErr != nil || id == "" {
			continue
		}
		distance := haversineMiles(latitude, longitude, lat, lon)
		if distance > radiusMiles*1.05 {
			continue
		}
		results = append(results, WaterSiteCandidate{SiteID: id, Name: abbreviate(clean(values["station_nm"]), 200), Latitude: lat, Longitude: lon, DistanceMiles: math.Round(distance*10) / 10})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].DistanceMiles < results[j].DistanceMiles })
	if len(results) > 50 {
		results = results[:50]
	}
	return results
}

func haversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMiles = 3958.8
	toRadians := func(value float64) float64 { return value * math.Pi / 180 }
	dLat, dLon := toRadians(lat2-lat1), toRadians(lon2-lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(toRadians(lat1))*math.Cos(toRadians(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusMiles * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func normalizeWaterSiteIDs(input string) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return []string{}, nil
	}
	seen := map[string]bool{}
	result := []string{}
	for _, raw := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
		id := strings.ToUpper(strings.TrimSpace(raw))
		if !waterSitePattern.MatchString(id) {
			return nil, validationError{"water gauge IDs may contain only letters, numbers, and hyphens"}
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	if len(result) > 25 {
		return nil, validationError{"configure no more than 25 water gauges"}
	}
	return result, nil
}

func (s *WaterService) Status(ctx context.Context) WaterStatus {
	settings := s.store.Snapshot().Settings.Water
	if !settings.Enabled {
		return WaterStatus{State: "disabled", Message: "Water monitoring is off", Source: "USGS Water Services", Gauges: []WaterGauge{}}
	}
	ids, err := normalizeWaterSiteIDs(settings.SiteIDs)
	if err != nil || len(ids) == 0 {
		return WaterStatus{State: "unavailable", Message: "Choose at least one nearby water gauge in Settings", Source: "USGS Water Services", Gauges: []WaterGauge{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	refresh := time.Duration(settings.RefreshMinutes) * time.Minute
	configurationKey := strings.Join([]string{settings.SiteIDs, settings.SourceURL, strconv.FormatBool(settings.NOAAEnabled), settings.NOAAURL}, "|")
	if s.cached.UpdatedAt != nil && s.cacheKey == configurationKey && time.Since(*s.cached.UpdatedAt) < refresh {
		return s.cached
	}
	status, err := s.fetch(ctx, settings, ids)
	if err == nil {
		if settings.NOAAEnabled {
			if enrichErr := s.enrichNOAA(ctx, settings, status.Gauges); enrichErr != nil {
				s.logger.Printf("NOAA water enrichment: %v", enrichErr)
				status.Message += " · NOAA enrichment unavailable"
			} else {
				status.Message += " · NOAA flood categories included"
			}
		}
		s.cached = status
		s.cacheKey = configurationKey
		if e := s.saveCache(status); e != nil {
			s.logger.Printf("water cache: %v", e)
		}
		return status
	}
	s.logger.Printf("water update: %v", err)
	if s.cached.UpdatedAt != nil {
		stale := s.cached
		stale.State = "stale"
		stale.Stale = true
		stale.Message = "Using cached water gauges; update failed: " + err.Error()
		return stale
	}
	return WaterStatus{State: "unavailable", Message: "Water gauges are unavailable: " + err.Error(), Source: "USGS Water Services", Gauges: []WaterGauge{}}
}

type noaaGaugeMetadata struct {
	LID                   string `json:"lid"`
	USGSID                string `json:"usgsId"`
	ObservedFloodCategory string `json:"ObservedFloodCategory"`
	ForecastFloodCategory string `json:"ForecastFloodCategory"`
	Flood                 struct {
		Categories struct {
			Minor struct {
				Stage *float64 `json:"stage"`
			} `json:"minor"`
		} `json:"categories"`
	} `json:"flood"`
}
type noaaStageFlow struct {
	Observed noaaStageSeries `json:"observed"`
	Forecast noaaStageSeries `json:"forecast"`
}
type noaaStageSeries struct {
	PrimaryName    string `json:"primaryName"`
	PrimaryUnits   string `json:"primaryUnits"`
	SecondaryName  string `json:"secondaryName"`
	SecondaryUnits string `json:"secondaryUnits"`
	Data           []struct {
		ValidTime time.Time `json:"validTime"`
		Primary   *float64  `json:"primary"`
		Secondary *float64  `json:"secondary"`
	} `json:"data"`
}

func (s *WaterService) enrichNOAA(ctx context.Context, settings WaterSettings, gauges []WaterGauge) error {
	base := strings.TrimRight(settings.NOAAURL, "/")
	if base == "" {
		base = defaultNOAAWaterURL
	}
	limit := min(len(gauges), 10)
	failures := 0
	for index := 0; index < limit; index++ {
		id := url.PathEscape(gauges[index].SiteID)
		var metadata noaaGaugeMetadata
		if err := s.fetchJSON(ctx, base+"/"+id, &metadata); err != nil {
			failures++
			continue
		}
		gauges[index].FloodCategory = strings.ToLower(clean(metadata.ObservedFloodCategory))
		gauges[index].ForecastFloodCategory = strings.ToLower(clean(metadata.ForecastFloodCategory))
		gauges[index].FloodStageFeet = metadata.Flood.Categories.Minor.Stage
		var stages noaaStageFlow
		if err := s.fetchJSON(ctx, base+"/"+id+"/stageflow", &stages); err != nil {
			failures++
			continue
		}
		if len(stages.Observed.Data) > 0 {
			latest := stages.Observed.Data[len(stages.Observed.Data)-1]
			if stage := noaaStageValue(stages.Observed, latest.Primary, latest.Secondary); stage != nil {
				gauges[index].StageFeet = stage
				gauges[index].ObservedAt = &latest.ValidTime
			}
		}
		if len(stages.Forecast.Data) > 0 {
			peakIndex := 0
			peak := -1.0
			for i, item := range stages.Forecast.Data {
				if value := noaaStageValue(stages.Forecast, item.Primary, item.Secondary); value != nil && *value > peak {
					peak = *value
					peakIndex = i
				}
			}
			item := stages.Forecast.Data[peakIndex]
			gauges[index].ForecastStageFeet = noaaStageValue(stages.Forecast, item.Primary, item.Secondary)
			gauges[index].ForecastAt = &item.ValidTime
		}
	}
	if failures == limit && limit > 0 {
		return errors.New("all NOAA gauge requests failed")
	}
	return nil
}

func noaaStageValue(series noaaStageSeries, primary, secondary *float64) *float64 {
	if strings.Contains(strings.ToLower(series.PrimaryName), "stage") || strings.EqualFold(series.PrimaryUnits, "ft") {
		return primary
	}
	if strings.Contains(strings.ToLower(series.SecondaryName), "stage") || strings.EqualFold(series.SecondaryUnits, "ft") {
		return secondary
	}
	return nil
}
func (s *WaterService) fetchJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version)
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(response.Body, maxWaterResponse)).Decode(target) != nil {
		return errors.New("provider returned invalid JSON")
	}
	return nil
}

type usgsPayload struct {
	Value struct {
		TimeSeries []struct {
			SourceInfo struct {
				SiteName string `json:"siteName"`
				SiteCode []struct {
					Value string `json:"value"`
				} `json:"siteCode"`
				GeoLocation struct {
					GeogLocation struct {
						Latitude  float64 `json:"latitude"`
						Longitude float64 `json:"longitude"`
					} `json:"geogLocation"`
				} `json:"geoLocation"`
			} `json:"sourceInfo"`
			Variable struct {
				VariableCode []struct {
					Value string `json:"value"`
				} `json:"variableCode"`
			} `json:"variable"`
			Values []struct {
				Value []struct {
					Value      string    `json:"value"`
					DateTime   time.Time `json:"dateTime"`
					Qualifiers []string  `json:"qualifiers"`
				} `json:"value"`
			} `json:"values"`
		} `json:"timeSeries"`
	} `json:"value"`
}

func (s *WaterService) fetch(ctx context.Context, settings WaterSettings, ids []string) (WaterStatus, error) {
	endpointText := settings.SourceURL
	if endpointText == "" {
		endpointText = defaultUSGSWaterURL
	}
	endpoint, err := url.Parse(endpointText)
	if err != nil {
		return WaterStatus{}, errors.New("water provider address is invalid")
	}
	q := endpoint.Query()
	q.Set("format", "json")
	q.Set("sites", strings.Join(ids, ","))
	q.Set("parameterCd", "00060,00065")
	q.Set("period", "PT6H")
	q.Set("siteStatus", "active")
	endpoint.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return WaterStatus{}, err
	}
	req.Header.Set("User-Agent", "TicketsLocal/"+version)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return WaterStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return WaterStatus{}, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	var payload usgsPayload
	if json.NewDecoder(io.LimitReader(resp.Body, maxWaterResponse)).Decode(&payload) != nil {
		return WaterStatus{}, errors.New("provider returned invalid water data")
	}
	gauges := map[string]*WaterGauge{}
	for _, series := range payload.Value.TimeSeries {
		if len(series.SourceInfo.SiteCode) == 0 || len(series.Variable.VariableCode) == 0 {
			continue
		}
		id := series.SourceInfo.SiteCode[0].Value
		g := gauges[id]
		if g == nil {
			g = &WaterGauge{SiteID: id, Name: abbreviate(clean(series.SourceInfo.SiteName), 200), Latitude: series.SourceInfo.GeoLocation.GeogLocation.Latitude, Longitude: series.SourceInfo.GeoLocation.GeogLocation.Longitude}
			gauges[id] = g
		}
		if len(series.Values) == 0 {
			continue
		}
		values := series.Values[0].Value
		valid := []struct {
			v float64
			t time.Time
			q []string
		}{}
		for _, item := range values {
			v, e := strconv.ParseFloat(item.Value, 64)
			if e == nil {
				valid = append(valid, struct {
					v float64
					t time.Time
					q []string
				}{v, item.DateTime, item.Qualifiers})
			}
		}
		if len(valid) == 0 {
			continue
		}
		latest := valid[len(valid)-1]
		trend := "steady"
		if len(valid) > 1 {
			delta := latest.v - valid[0].v
			if delta > .02 {
				trend = "rising"
			} else if delta < -.02 {
				trend = "falling"
			}
		}
		g.ObservedAt = &latest.t
		for _, q := range latest.q {
			if strings.EqualFold(q, "P") {
				g.Provisional = true
			}
		}
		switch series.Variable.VariableCode[0].Value {
		case "00065":
			g.StageFeet = pointer(latest.v)
			g.StageTrend = trend
		case "00060":
			g.FlowCFS = pointer(latest.v)
			g.FlowTrend = trend
		}
	}
	result := make([]WaterGauge, 0, len(gauges))
	for _, g := range gauges {
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	now := time.Now().UTC()
	source := "USGS Water Services"
	if settings.SourceURL != "" {
		source = "Configured water source (" + endpoint.Hostname() + ")"
	}
	return WaterStatus{State: "current", Message: fmt.Sprintf("%d water gauge(s) updated", len(result)), UpdatedAt: &now, Source: source, Gauges: result}, nil
}

func (s *WaterService) loadCache() {
	b, e := os.ReadFile(s.cachePath)
	if e != nil {
		return
	}
	var v WaterStatus
	if json.Unmarshal(b, &v) == nil && v.UpdatedAt != nil {
		v.State = "stale"
		v.Stale = true
		v.Message = "Using water gauges saved from the previous session"
		if v.Gauges == nil {
			v.Gauges = []WaterGauge{}
		}
		s.cached = v
	}
}
func (s *WaterService) saveCache(v WaterStatus) error {
	f, e := os.CreateTemp(filepath.Dir(s.cachePath), ".water-*.json")
	if e != nil {
		return e
	}
	p := f.Name()
	defer os.Remove(p)
	if e = f.Chmod(0600); e == nil {
		e = json.NewEncoder(f).Encode(v)
	}
	if e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(p, s.cachePath)
}
