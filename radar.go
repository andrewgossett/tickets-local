package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultNWSRadarURL = "https://mapservices.weather.noaa.gov/eventdriven/rest/services/radar/radar_base_reflectivity/MapServer/export"
	maxRadarResponse   = 6 << 20
	webMercatorLimit   = 20037509.0
)

type radarCacheEntry struct {
	image     []byte
	updatedAt time.Time
	fetchedAt time.Time
}

func (service *WeatherService) Radar(ctx context.Context, query url.Values) ([]byte, time.Time, bool, error) {
	settings := service.store.Snapshot().Settings.Weather
	if !settings.Enabled || !settings.RadarEnabled {
		return nil, time.Time{}, false, errors.New("weather radar is disabled")
	}
	bbox, width, height, frameTime, err := validateRadarRequest(query)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	frameKey := "current"
	if frameTime != nil {
		frameKey = strconv.FormatInt(frameTime.UnixMilli(), 10)
	}
	keyBytes := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%s", bbox, width, height, frameKey)))
	key := hex.EncodeToString(keyBytes[:])

	service.mu.Lock()
	if entry, ok := service.radarCache[key]; ok && time.Since(entry.fetchedAt) < 5*time.Minute {
		image := append([]byte(nil), entry.image...)
		service.mu.Unlock()
		return image, entry.updatedAt, false, nil
	}
	service.mu.Unlock()

	image, fetchErr := service.fetchRadar(ctx, bbox, width, height, frameTime)
	if fetchErr == nil {
		now := time.Now().UTC()
		observationTime := now
		if frameTime != nil {
			observationTime = *frameTime
		}
		service.mu.Lock()
		service.radarCache[key] = radarCacheEntry{image: append([]byte(nil), image...), updatedAt: observationTime, fetchedAt: now}
		if len(service.radarCache) > 12 {
			for cachedKey := range service.radarCache {
				if cachedKey != key {
					delete(service.radarCache, cachedKey)
					break
				}
			}
		}
		service.mu.Unlock()
		return image, observationTime, false, nil
	}
	service.mu.Lock()
	entry, ok := service.radarCache[key]
	service.mu.Unlock()
	if ok {
		return append([]byte(nil), entry.image...), entry.updatedAt, true, nil
	}
	return nil, time.Time{}, false, fmt.Errorf("weather radar is unavailable: %w", fetchErr)
}

func validateRadarRequest(query url.Values) (string, int, int, *time.Time, error) {
	parts := strings.Split(query.Get("bbox"), ",")
	if len(parts) != 4 {
		return "", 0, 0, nil, validationError{"radar bbox must contain four Web Mercator coordinates"}
	}
	values := make([]float64, 4)
	for index, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || value < -webMercatorLimit || value > webMercatorLimit {
			return "", 0, 0, nil, validationError{"radar bbox contains an invalid Web Mercator coordinate"}
		}
		values[index] = value
	}
	if values[0] >= values[2] || values[1] >= values[3] {
		return "", 0, 0, nil, validationError{"radar bbox bounds are reversed or empty"}
	}
	width, err := strconv.Atoi(query.Get("width"))
	if err != nil || width < 128 || width > 1400 {
		return "", 0, 0, nil, validationError{"radar width must be between 128 and 1,400 pixels"}
	}
	height, err := strconv.Atoi(query.Get("height"))
	if err != nil || height < 128 || height > 1000 {
		return "", 0, 0, nil, validationError{"radar height must be between 128 and 1,000 pixels"}
	}
	var frameTime *time.Time
	if raw := query.Get("time"); raw != "" {
		milliseconds, err := strconv.ParseInt(raw, 10, 64)
		candidate := time.UnixMilli(milliseconds).UTC()
		now := time.Now().UTC()
		if err != nil || milliseconds%300000 != 0 || candidate.Before(now.Add(-2*time.Hour)) || candidate.After(now.Add(5*time.Minute)) {
			return "", 0, 0, nil, validationError{"radar animation time must be a recent five-minute boundary"}
		}
		frameTime = &candidate
	}
	return strings.Join(parts, ","), width, height, frameTime, nil
}

func (service *WeatherService) fetchRadar(ctx context.Context, bbox string, width, height int, frameTime *time.Time) ([]byte, error) {
	endpointText := service.store.Snapshot().Settings.Weather.RadarURL
	if endpointText == "" {
		endpointText = strings.TrimSpace(os.Getenv("TICKETS_LOCAL_NWS_RADAR_URL"))
	}
	if endpointText == "" {
		endpointText = defaultNWSRadarURL
	}
	endpoint, err := url.Parse(endpointText)
	if err != nil || !oneOf(endpoint.Scheme, "http", "https") || endpoint.Host == "" {
		return nil, errors.New("radar provider address is invalid")
	}
	query := endpoint.Query()
	query.Set("bbox", bbox)
	query.Set("bboxSR", "3857")
	query.Set("imageSR", "3857")
	query.Set("size", fmt.Sprintf("%d,%d", width, height))
	query.Set("format", "png32")
	query.Set("transparent", "true")
	query.Set("f", "image")
	if frameTime != nil {
		query.Set("time", strconv.FormatInt(frameTime.UnixMilli(), 10))
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("could not prepare radar request")
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	request.Header.Set("Accept", "image/png")
	response, err := service.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	if contentType := strings.ToLower(response.Header.Get("Content-Type")); !strings.HasPrefix(contentType, "image/png") {
		return nil, errors.New("provider returned a non-PNG radar response")
	}
	image, err := io.ReadAll(io.LimitReader(response.Body, maxRadarResponse+1))
	if err != nil || len(image) == 0 || len(image) > maxRadarResponse {
		return nil, errors.New("provider returned an invalid radar image")
	}
	return image, nil
}
