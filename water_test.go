package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const usgsFixture = `{"value":{"timeSeries":[
{"sourceInfo":{"siteName":"Harpeth River at Franklin","siteCode":[{"value":"03432350"}],"geoLocation":{"geogLocation":{"latitude":35.92,"longitude":-86.87}}},"variable":{"variableCode":[{"value":"00065"}]},"values":[{"value":[{"value":"4.10","dateTime":"2026-09-06T10:00:00Z","qualifiers":["P"]},{"value":"4.45","dateTime":"2026-09-06T15:00:00Z","qualifiers":["P"]}]}]},
{"sourceInfo":{"siteName":"Harpeth River at Franklin","siteCode":[{"value":"03432350"}],"geoLocation":{"geogLocation":{"latitude":35.92,"longitude":-86.87}}},"variable":{"variableCode":[{"value":"00060"}]},"values":[{"value":[{"value":"200","dateTime":"2026-09-06T10:00:00Z","qualifiers":["P"]},{"value":"180","dateTime":"2026-09-06T15:00:00Z","qualifiers":["P"]}]}]}
]}}`

const usgsSiteFixture = `# USGS site search
agency_cd	site_no	station_nm	site_tp_cd	dec_lat_va	dec_long_va
5s	15s	50s	7s	16n	16n
USGS	03432350	HARPETH RIVER AT FRANKLIN, TN	ST	35.9200	-86.8700
USGS	03431000	HARPETH RIVER AT KINGSTON SPRINGS, TN	ST	36.1000	-87.1000
`

func TestWaterServiceDiscoversNearbyNamedSites(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("format") != "rdb" || query.Get("siteType") != "ST" || query.Get("hasDataTypeCd") != "iv" || query.Get("bBox") == "" {
			t.Errorf("unexpected site query: %v", query)
		}
		_, _ = io.WriteString(w, usgsSiteFixture)
	}))
	defer provider.Close()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewWaterService(store, provider.Client(), log.New(io.Discard, "", 0))
	service.siteURL = provider.URL
	sites, err := service.DiscoverSites(context.Background(), 35.925, -86.868, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 || sites[0].SiteID != "03432350" || sites[0].Name != "HARPETH RIVER AT FRANKLIN, TN" || sites[0].DistanceMiles <= 0 {
		t.Fatalf("unexpected discovered sites: %+v", sites)
	}
	if _, err = service.DiscoverSites(context.Background(), 35.9, -86.8, 500); err == nil {
		t.Fatal("oversized gauge search radius accepted")
	}
}

func TestWaterServiceFetchesTrendsAndCachesOutsideEventLog(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sites") != "03432350" || r.URL.Query().Get("period") != "PT6H" || r.URL.Query().Get("parameterCd") != "00060,00065" {
			t.Errorf("unexpected query: %v", r.URL.Query())
		}
		_, _ = io.WriteString(w, usgsFixture)
	}))
	defer provider.Close()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Water = WaterSettings{Enabled: true, SiteIDs: "03432350", RefreshMinutes: 15, SourceURL: provider.URL}
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	service := NewWaterService(store, provider.Client(), log.New(io.Discard, "", 0))
	status := service.Status(context.Background())
	if status.State != "current" || len(status.Gauges) != 1 {
		t.Fatalf("unexpected status: %+v", status)
	}
	g := status.Gauges[0]
	if g.StageFeet == nil || *g.StageFeet != 4.45 || g.StageTrend != "rising" || g.FlowCFS == nil || *g.FlowCFS != 180 || g.FlowTrend != "falling" || !g.Provisional {
		t.Fatalf("unexpected gauge: %+v", g)
	}
	if _, err = os.Stat(filepath.Join(store.dataDir, "water-cache.json")); err != nil {
		t.Fatal(err)
	}
	var events strings.Builder
	if err = store.EventLog(&events); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(events.String(), "Harpeth River") {
		t.Fatal("water data entered event log")
	}
}

func TestWaterSettingsBoundGaugeCountAndIDs(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.Water.SiteIDs = "03432350, bad/id"
	if _, err = store.UpdateSettings(settings); err == nil {
		t.Fatal("invalid gauge ID accepted")
	}
	ids := make([]string, 26)
	for i := range ids {
		ids[i] = "SITE" + string(rune('A'+i))
	}
	settings.Water.SiteIDs = strings.Join(ids, ",")
	if _, err = store.UpdateSettings(settings); err == nil {
		t.Fatal("too many gauges accepted")
	}
}

func TestWaterServiceEnrichesNOAAFloodAndForecastData(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/03432350":
			_, _ = io.WriteString(w, `{"lid":"FRAT1","usgsId":"03432350","ObservedFloodCategory":"minor","ForecastFloodCategory":"moderate","flood":{"categories":{"minor":{"stage":12}}}}`)
		case "/03432350/stageflow":
			_, _ = io.WriteString(w, `{"observed":{"primaryName":"Stage","primaryUnits":"ft","data":[{"validTime":"2026-09-07T12:00:00Z","primary":12.4}]},"forecast":{"primaryName":"Stage","primaryUnits":"ft","data":[{"validTime":"2026-09-07T18:00:00Z","primary":13.1},{"validTime":"2026-09-08T00:00:00Z","primary":14.6},{"validTime":"2026-09-08T06:00:00Z","primary":13.8}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewWaterService(store, provider.Client(), log.New(io.Discard, "", 0))
	gauges := []WaterGauge{{SiteID: "03432350"}}
	if err = service.enrichNOAA(context.Background(), WaterSettings{NOAAURL: provider.URL}, gauges); err != nil {
		t.Fatal(err)
	}
	g := gauges[0]
	if g.FloodCategory != "minor" || g.ForecastFloodCategory != "moderate" || g.FloodStageFeet == nil || *g.FloodStageFeet != 12 || g.StageFeet == nil || *g.StageFeet != 12.4 || g.ForecastStageFeet == nil || *g.ForecastStageFeet != 14.6 || g.ForecastAt == nil {
		t.Fatalf("unexpected NOAA enrichment: %+v", g)
	}
}
