package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxMapPackTiles = 2500

type MapPack struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CenterLat   float64   `json:"center_lat"`
	CenterLon   float64   `json:"center_lon"`
	RadiusMiles float64   `json:"radius_miles"`
	MinZoom     int       `json:"min_zoom"`
	MaxZoom     int       `json:"max_zoom"`
	TileCount   int       `json:"tile_count"`
	Bytes       int64     `json:"bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

type MapPackInput struct {
	Name        string  `json:"name"`
	CenterLat   float64 `json:"center_lat"`
	CenterLon   float64 `json:"center_lon"`
	RadiusMiles float64 `json:"radius_miles"`
	MinZoom     int     `json:"min_zoom"`
	MaxZoom     int     `json:"max_zoom"`
}

type MapTileService struct {
	mu       sync.Mutex
	root     string
	endpoint string
	client   *http.Client
	packs    []MapPack
}

func NewMapTileService(dataDir string, client *http.Client) (*MapTileService, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("TICKETS_LOCAL_TILE_URL")), "/")
	if endpoint == "" {
		endpoint = "https://tile.openstreetmap.org"
	}
	service := &MapTileService{root: filepath.Join(dataDir, "map-tiles"), endpoint: endpoint, client: client, packs: []MapPack{}}
	if err := os.MkdirAll(service.root, 0o700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(service.root, "packs.json"))
	if err == nil {
		if err := json.Unmarshal(data, &service.packs); err != nil {
			return nil, fmt.Errorf("read map packs: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return service, nil
}

func (s *MapTileService) Packs() []MapPack {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MapPack{}, s.packs...)
}

func (s *MapTileService) Tile(ctx context.Context, zoom, x, y int) ([]byte, bool, error) {
	if zoom < 2 || zoom > 18 || x < 0 || y < 0 || x >= 1<<zoom || y >= 1<<zoom {
		return nil, false, validationError{"invalid map tile coordinates"}
	}
	file := s.tilePath(zoom, x, y)
	if data, err := os.ReadFile(file); err == nil {
		return data, true, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%d/%d/%d.png", s.endpoint, zoom, x, y), nil)
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("User-Agent", "TicketsLocal/"+version+" (+https://github.com/openises/tickets)")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "image/png") {
		return nil, false, fmt.Errorf("map provider returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, false, errors.New("map provider returned an invalid tile")
	}
	if err := writePrivateFile(file, data); err != nil {
		return nil, false, err
	}
	return data, false, nil
}

func (s *MapTileService) CreatePack(ctx context.Context, input MapPackInput) (MapPack, error) {
	input.Name = clean(input.Name)
	if input.Name == "" || len(input.Name) > 100 || math.Abs(input.CenterLat) > 85 || math.Abs(input.CenterLon) > 180 || input.RadiusMiles < 1 || input.RadiusMiles > 100 || input.MinZoom < 2 || input.MaxZoom > 16 || input.MaxZoom < input.MinZoom {
		return MapPack{}, validationError{"enter a name, valid center, 1–100 mile radius, and zoom range 2–16"}
	}
	tiles := packTiles(input)
	if len(tiles) > maxMapPackTiles {
		return MapPack{}, validationError{fmt.Sprintf("map pack would contain %d tiles; reduce the radius or zoom below the %d tile limit", len(tiles), maxMapPackTiles)}
	}
	pack := MapPack{ID: newID("map-pack"), Name: input.Name, CenterLat: input.CenterLat, CenterLon: input.CenterLon, RadiusMiles: input.RadiusMiles, MinZoom: input.MinZoom, MaxZoom: input.MaxZoom, CreatedAt: time.Now().UTC()}
	for _, tile := range tiles {
		data, _, err := s.Tile(ctx, tile[0], tile[1], tile[2])
		if err != nil {
			return MapPack{}, fmt.Errorf("download map tile %d/%d/%d: %w", tile[0], tile[1], tile[2], err)
		}
		pack.TileCount++
		pack.Bytes += int64(len(data))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packs = append(s.packs, pack)
	if err := s.saveLocked(); err != nil {
		s.packs = s.packs[:len(s.packs)-1]
		return MapPack{}, err
	}
	return pack, nil
}

func (s *MapTileService) DeletePack(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, pack := range s.packs {
		if pack.ID != id {
			continue
		}
		s.packs = append(s.packs[:index], s.packs[index+1:]...)
		return s.saveLocked()
	}
	return os.ErrNotExist
}

func (s *MapTileService) saveLocked() error {
	sort.SliceStable(s.packs, func(i, j int) bool { return s.packs[i].CreatedAt.After(s.packs[j].CreatedAt) })
	data, err := json.MarshalIndent(s.packs, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(s.root, "packs.json"), data)
}

func (s *MapTileService) tilePath(zoom, x, y int) string {
	return filepath.Join(s.root, "cache", strconv.Itoa(zoom), strconv.Itoa(x), strconv.Itoa(y)+".png")
}

func writePrivateFile(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return err
	}
	temporary := name + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, name)
}

func packTiles(input MapPackInput) [][3]int {
	latDelta := input.RadiusMiles / 69.0
	lonDelta := input.RadiusMiles / math.Max(10, 69*math.Cos(input.CenterLat*math.Pi/180))
	result := make([][3]int, 0)
	for zoom := input.MinZoom; zoom <= input.MaxZoom; zoom++ {
		minX, maxY := lonLatTile(input.CenterLon-lonDelta, input.CenterLat-latDelta, zoom)
		maxX, minY := lonLatTile(input.CenterLon+lonDelta, input.CenterLat+latDelta, zoom)
		for x := minX; x <= maxX; x++ {
			for y := minY; y <= maxY; y++ {
				result = append(result, [3]int{zoom, x, y})
			}
		}
	}
	return result
}

func lonLatTile(longitude, latitude float64, zoom int) (int, int) {
	n := math.Exp2(float64(zoom))
	latitude = math.Max(-85.05112878, math.Min(85.05112878, latitude))
	x := int(math.Floor((longitude + 180) / 360 * n))
	latRad := latitude * math.Pi / 180
	y := int(math.Floor((1 - math.Asinh(math.Tan(latRad))/math.Pi) / 2 * n))
	limit := int(n) - 1
	return max(0, min(limit, x)), max(0, min(limit, y))
}

func mapTilePathValues(pathText string) (int, int, int, error) {
	parts := strings.Split(strings.Trim(strings.TrimSuffix(strings.TrimPrefix(pathText, "/api/map/tiles/"), ".png"), "/"), "/")
	if len(parts) != 3 {
		return 0, 0, 0, validationError{"invalid map tile path"}
	}
	values := make([]int, 3)
	for index := range parts {
		value, err := strconv.Atoi(parts[index])
		if err != nil {
			return 0, 0, 0, validationError{"invalid map tile path"}
		}
		values[index] = value
	}
	return values[0], values[1], values[2], nil
}
