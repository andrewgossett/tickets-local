package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutingService(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "-86.700000,35.900000;-86.600000,36.000000") || r.URL.Query().Get("geometries") != "geojson" {
			t.Fatalf("unexpected routing request: %s", r.URL.String())
		}
		_, _ = io.WriteString(w, "{\"code\":\"Ok\",\"routes\":[{\"distance\":1609.344,\"duration\":600,\"geometry\":{\"coordinates\":[[-86.7,35.9],[-86.65,35.95],[-86.6,36.0]]}}]}")
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_ROUTE_URL", provider.URL)
	result, err := NewRoutingService(provider.Client()).Route(context.Background(), RouteInput{Start: MapCoordinate{Latitude: 35.9, Longitude: -86.7}, End: MapCoordinate{Latitude: 36, Longitude: -86.6}})
	if err != nil || result.DistanceMiles != 1 || result.DurationMinutes != 10 || len(result.Path) != 3 {
		t.Fatalf("unexpected route: %+v err=%v", result, err)
	}
}

func TestRoutingRejectsInvalidCoordinates(t *testing.T) {
	if _, err := NewRoutingService(&http.Client{}).Route(context.Background(), RouteInput{Start: MapCoordinate{Latitude: 90}, End: MapCoordinate{Latitude: 35}}); err == nil {
		t.Fatal("expected invalid route to be rejected")
	}
}
