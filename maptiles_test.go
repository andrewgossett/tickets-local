package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

var validMapTilePNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}

func TestMapTileCachesForOfflineUse(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(validMapTilePNG)
	}))
	t.Cleanup(provider.Close)
	t.Setenv("TICKETS_LOCAL_TILE_URL", provider.URL)
	service, err := NewMapTileService(t.TempDir(), provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	data, cached, err := service.Tile(context.Background(), 4, 3, 6)
	if err != nil || cached || string(data) != string(validMapTilePNG) {
		t.Fatalf("unexpected first tile: cached=%v data=%x err=%v", cached, data, err)
	}
	provider.Close()
	data, cached, err = service.Tile(context.Background(), 4, 3, 6)
	if err != nil || !cached || string(data) != string(validMapTilePNG) || requests != 1 {
		t.Fatalf("offline cache failed: cached=%v requests=%d err=%v", cached, requests, err)
	}
}

func TestMapPackDownloadsAndPersistsMetadata(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(validMapTilePNG)
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_TILE_URL", provider.URL)
	dataDir := t.TempDir()
	service, _ := NewMapTileService(dataDir, provider.Client())
	pack, err := service.CreatePack(context.Background(), MapPackInput{Name: "EOC", CenterLat: 35.97, CenterLon: -86.66, RadiusMiles: 5, MinZoom: 8, MaxZoom: 9})
	if err != nil || pack.TileCount == 0 || pack.Bytes == 0 {
		t.Fatalf("unexpected pack: %+v err=%v", pack, err)
	}
	reopened, err := NewMapTileService(dataDir, provider.Client())
	if err != nil || len(reopened.Packs()) != 1 || reopened.Packs()[0].ID != pack.ID {
		t.Fatalf("pack metadata did not persist: %+v err=%v", reopened.Packs(), err)
	}
	if err := reopened.DeletePack(pack.ID); err != nil || len(reopened.Packs()) != 0 {
		t.Fatalf("pack deletion failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "map-tiles", "packs.json")); err != nil {
		t.Fatal(err)
	}
}

func TestMapPackLimits(t *testing.T) {
	service, _ := NewMapTileService(t.TempDir(), &http.Client{})
	if service.Packs() == nil {
		t.Fatal("empty map-pack list must encode as an array")
	}
	if _, err := service.CreatePack(context.Background(), MapPackInput{Name: "Too large", CenterLat: 35, CenterLon: -86, RadiusMiles: 100, MinZoom: 2, MaxZoom: 16}); err == nil {
		t.Fatal("expected oversized pack to be rejected")
	}
	if _, _, err := service.Tile(context.Background(), 19, 0, 0); err == nil {
		t.Fatal("expected invalid tile to be rejected")
	}
}

func TestMapTileAPI(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(validMapTilePNG)
	}))
	defer provider.Close()
	t.Setenv("TICKETS_LOCAL_TILE_URL", provider.URL)
	store, _ := OpenStore(t.TempDir())
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/map/tiles/4/3/6.png", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/png" || string(data) != string(validMapTilePNG) {
		t.Fatalf("unexpected response: %d %s %x", response.StatusCode, response.Header.Get("Content-Type"), data)
	}
}

func TestMapDrawingAPI(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	input := MapOverlayInput{Name: "Blocked road", Color: "#f97316", Visible: true, Features: []OverlayFeature{{Name: "Blocked road", GeometryType: "line", Paths: [][]MapCoordinate{{{Latitude: 35.9, Longitude: -86.7}, {Latitude: 35.91, Longitude: -86.71}}}}}}
	overlay := requestJSON[MapOverlay](t, httpServer.URL+"/api/overlays", http.MethodPost, input, http.StatusCreated)
	if overlay.FileName != "Map drawing" || len(store.Snapshot().Overlays) != 1 {
		t.Fatalf("drawing was not persisted: %+v", overlay)
	}
}
