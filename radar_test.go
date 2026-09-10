package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testRadarPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}

func radarQuery() url.Values {
	return url.Values{"bbox": {"-1000000,-500000,1000000,500000"}, "width": {"800"}, "height": {"500"}}
}

func radarTestService(t *testing.T, provider *httptest.Server) *WeatherService {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Weather.Enabled = true
	settings.Weather.RadarEnabled = true
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TICKETS_LOCAL_NWS_RADAR_URL", provider.URL)
	return NewWeatherService(store, provider.Client(), log.New(io.Discard, "", 0))
}

func TestWeatherRadarFetchesAndCachesPNG(t *testing.T) {
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		query := r.URL.Query()
		if query.Get("bboxSR") != "3857" || query.Get("imageSR") != "3857" || query.Get("size") != "800,500" || query.Get("transparent") != "true" {
			t.Errorf("unexpected radar provider query: %v", query)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testRadarPNG)
	}))
	defer provider.Close()
	service := radarTestService(t, provider)

	image, updatedAt, stale, err := service.Radar(context.Background(), radarQuery())
	if err != nil || stale || updatedAt.IsZero() || string(image) != string(testRadarPNG) {
		t.Fatalf("unexpected radar response: bytes=%d time=%v stale=%v err=%v", len(image), updatedAt, stale, err)
	}
	if _, _, _, err := service.Radar(context.Background(), radarQuery()); err != nil || requests.Load() != 1 {
		t.Fatalf("radar cache was not reused: requests=%d err=%v", requests.Load(), err)
	}
}

func TestWeatherRadarUsesMemoryCacheWhenRefreshFails(t *testing.T) {
	fail := atomic.Bool{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testRadarPNG)
	}))
	defer provider.Close()
	service := radarTestService(t, provider)
	if _, _, _, err := service.Radar(context.Background(), radarQuery()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	for key, entry := range service.radarCache {
		entry.fetchedAt = time.Now().UTC().Add(-10 * time.Minute)
		service.radarCache[key] = entry
	}
	service.mu.Unlock()
	fail.Store(true)
	image, _, stale, err := service.Radar(context.Background(), radarQuery())
	if err != nil || !stale || string(image) != string(testRadarPNG) {
		t.Fatalf("cached radar fallback failed: bytes=%d stale=%v err=%v", len(image), stale, err)
	}
}

func TestWeatherRadarRejectsInvalidRequestsAndResponses(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer provider.Close()
	service := radarTestService(t, provider)
	invalid := radarQuery()
	invalid.Set("bbox", "10,10,5,5")
	if _, _, _, err := service.Radar(context.Background(), invalid); err == nil || !strings.Contains(err.Error(), "reversed") {
		t.Fatalf("invalid bounds error = %v", err)
	}
	if _, _, _, err := service.Radar(context.Background(), radarQuery()); err == nil || !strings.Contains(err.Error(), "non-PNG") {
		t.Fatalf("invalid provider response error = %v", err)
	}
}

func TestWeatherRadarAPIProxiesHostImage(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testRadarPNG)
	}))
	defer provider.Close()
	service := radarTestService(t, provider)
	apiServer, err := newAPIServer(service.store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	apiServer.weather = service
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/weather/radar?"+radarQuery().Encode(), nil)
	apiServer.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || recorder.Header().Get("X-Tickets-Weather-Time") == "" {
		t.Fatalf("unexpected radar API response: status=%d headers=%v", recorder.Code, recorder.Header())
	}
}

func TestWeatherRadarAcceptsRecentBoundedAnimationFrame(t *testing.T) {
	var providerTime string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerTime = r.URL.Query().Get("time")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testRadarPNG)
	}))
	defer provider.Close()
	service := radarTestService(t, provider)
	boundary := time.Now().UTC().Truncate(5 * time.Minute)
	query := radarQuery()
	query.Set("time", fmt.Sprint(boundary.UnixMilli()))
	_, observedAt, _, err := service.Radar(context.Background(), query)
	if err != nil || !observedAt.Equal(boundary) || providerTime != fmt.Sprint(boundary.UnixMilli()) {
		t.Fatalf("animation frame mismatch: observed=%v provider=%q err=%v", observedAt, providerTime, err)
	}
	query.Set("time", fmt.Sprint(time.Now().UTC().Add(-3*time.Hour).UnixMilli()))
	if _, _, _, err := service.Radar(context.Background(), query); err == nil {
		t.Fatal("radar accepted an animation frame outside the two-hour window")
	}
}
