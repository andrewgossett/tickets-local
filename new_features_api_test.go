package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMapDrawingAndAssetAPI(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	server, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	drawing := MapOverlayInput{Name: "Blocked road", Color: "#FF6600", Visible: true, Features: []OverlayFeature{{Name: "x road", GeometryType: "line", Paths: [][]MapCoordinate{{{Latitude: 35.9, Longitude: -86.7}, {Latitude: 36, Longitude: -86.6}}}}}}
	recorder := jsonRequest(t, handler, http.MethodPost, "/api/overlays", drawing)
	if recorder.Code != http.StatusCreated || len(store.Snapshot().Overlays) != 1 || store.Snapshot().Overlays[0].FileName != "Map drawing" {
		t.Fatalf("drawing API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	assetInput := AssetInput{Category: "equipment", Name: "Portable radios", Status: "ready", Quantity: 12, Location: "Radio cache"}
	recorder = jsonRequest(t, handler, http.MethodPost, "/api/assets", assetInput)
	if recorder.Code != http.StatusCreated || len(store.Snapshot().Assets) != 1 {
		t.Fatalf("asset create API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	asset := store.Snapshot().Assets[0]
	assetInput.Status = "assigned"
	assetInput.ExpectedUpdatedAt = &asset.UpdatedAt
	recorder = jsonRequest(t, handler, http.MethodPut, "/api/assets/"+asset.ID, assetInput)
	if recorder.Code != http.StatusOK || store.Snapshot().Assets[0].Status != "assigned" {
		t.Fatalf("asset update API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = jsonRequest(t, handler, http.MethodDelete, "/api/assets/"+asset.ID, nil)
	if recorder.Code != http.StatusOK || len(store.Snapshot().Assets) != 0 {
		t.Fatalf("asset delete API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	start := time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)
	scheduleInput := ScheduleItemInput{Title: "Operations shift", Type: "shift", StartAt: start, EndAt: start.Add(8 * time.Hour)}
	recorder = jsonRequest(t, handler, http.MethodPost, "/api/schedule", scheduleInput)
	if recorder.Code != http.StatusCreated || len(store.Snapshot().Schedule) != 1 {
		t.Fatalf("schedule create API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	item := store.Snapshot().Schedule[0]
	scheduleInput.Status = "confirmed"
	scheduleInput.ExpectedUpdatedAt = &item.UpdatedAt
	recorder = jsonRequest(t, handler, http.MethodPut, "/api/schedule/"+item.ID, scheduleInput)
	if recorder.Code != http.StatusOK || store.Snapshot().Schedule[0].Status != "confirmed" {
		t.Fatalf("schedule update API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = jsonRequest(t, handler, http.MethodDelete, "/api/schedule/"+item.ID, nil)
	if recorder.Code != http.StatusOK || len(store.Snapshot().Schedule) != 0 {
		t.Fatalf("schedule delete API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	responder, err := store.CreateResponder(ResponderInput{Name: "Training Operator", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	qualificationInput := QualificationInput{ResponderID: responder.ID, Category: "course", Name: "ICS-100", Status: "current"}
	recorder = jsonRequest(t, handler, http.MethodPost, "/api/qualifications", qualificationInput)
	if recorder.Code != http.StatusCreated || len(store.Snapshot().Qualifications) != 1 {
		t.Fatalf("qualification create API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	qualification := store.Snapshot().Qualifications[0]
	qualificationInput.Status = "expired"
	qualificationInput.ExpectedUpdatedAt = &qualification.UpdatedAt
	recorder = jsonRequest(t, handler, http.MethodPut, "/api/qualifications/"+qualification.ID, qualificationInput)
	if recorder.Code != http.StatusOK || store.Snapshot().Qualifications[0].Status != "expired" {
		t.Fatalf("qualification update API failed: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = jsonRequest(t, handler, http.MethodDelete, "/api/qualifications/"+qualification.ID, nil)
	if recorder.Code != http.StatusOK || len(store.Snapshot().Qualifications) != 0 {
		t.Fatalf("qualification delete API failed: %d %s", recorder.Code, recorder.Body.String())
	}
}

func jsonRequest(t *testing.T, handler http.Handler, method, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if value != nil {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
