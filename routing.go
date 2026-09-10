package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type RouteInput struct {
	Start MapCoordinate `json:"start"`
	End   MapCoordinate `json:"end"`
}

type RouteResult struct {
	DistanceMiles   float64         `json:"distance_miles"`
	DurationMinutes float64         `json:"duration_minutes"`
	Path            []MapCoordinate `json:"path"`
	Source          string          `json:"source"`
}

type RoutingService struct {
	client   *http.Client
	endpoint string
}

func NewRoutingService(client *http.Client) *RoutingService {
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("TICKETS_LOCAL_ROUTE_URL")), "/")
	if endpoint == "" {
		endpoint = "https://router.project-osrm.org/route/v1/driving"
	}
	return &RoutingService{client: client, endpoint: endpoint}
}

func (s *RoutingService) Route(ctx context.Context, input RouteInput) (RouteResult, error) {
	if !validMapCoordinate(input.Start) || !validMapCoordinate(input.End) {
		return RouteResult{}, validationError{"route endpoints must contain valid coordinates"}
	}
	endpoint := fmt.Sprintf("%s/%.6f,%.6f;%.6f,%.6f", s.endpoint, input.Start.Longitude, input.Start.Latitude, input.End.Longitude, input.End.Latitude)
	parsed, err := url.Parse(endpoint)
	if err != nil || !oneOf(parsed.Scheme, "http", "https") || parsed.Host == "" || parsed.User != nil {
		return RouteResult{}, errors.New("routing provider address is invalid")
	}
	query := parsed.Query()
	query.Set("overview", "full")
	query.Set("geometries", "geojson")
	query.Set("steps", "false")
	parsed.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	response, err := s.client.Do(request)
	if err != nil {
		return RouteResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return RouteResult{}, fmt.Errorf("routing provider returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Code   string `json:"code"`
		Routes []struct {
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
			Geometry struct {
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil || payload.Code != "Ok" || len(payload.Routes) == 0 {
		return RouteResult{}, errors.New("routing provider did not return a route")
	}
	if len(payload.Routes[0].Geometry.Coordinates) < 2 || len(payload.Routes[0].Geometry.Coordinates) > 20000 {
		return RouteResult{}, errors.New("routing provider returned invalid geometry")
	}
	result := RouteResult{DistanceMiles: payload.Routes[0].Distance / 1609.344, DurationMinutes: payload.Routes[0].Duration / 60, Source: parsed.Hostname()}
	for _, coordinate := range payload.Routes[0].Geometry.Coordinates {
		if len(coordinate) < 2 {
			return RouteResult{}, errors.New("routing provider returned invalid geometry")
		}
		point := MapCoordinate{Latitude: coordinate[1], Longitude: coordinate[0]}
		if !validMapCoordinate(point) {
			return RouteResult{}, errors.New("routing provider returned invalid geometry")
		}
		result.Path = append(result.Path, point)
	}
	return result, nil
}

func validMapCoordinate(point MapCoordinate) bool {
	return point.Latitude >= -85.05112878 && point.Latitude <= 85.05112878 && point.Longitude >= -180 && point.Longitude <= 180
}
