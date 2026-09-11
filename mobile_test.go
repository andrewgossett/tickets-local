package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMobileEnrollmentIsSingleUseAndStoresOnlyHashes(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenMobileAccessStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	enrollmentURL, expires, image, err := store.CreateEnrollment("unit-1", "http://192.168.1.50:8787", now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(enrollmentURL)
	token := parsed.Query().Get("enroll")
	if token == "" || parsed.Query().Get("view") != "mobile" || expires.Sub(now) != 10*time.Minute {
		t.Fatalf("unexpected enrollment: url=%q expires=%s", enrollmentURL, expires)
	}
	if !strings.HasPrefix(image, "data:image/png;base64,") {
		t.Fatalf("QR image is not an embedded PNG: %.30q", image)
	}
	device, key, err := store.Redeem(token, "Field phone", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if device.ResponderID != "unit-1" || device.KeyHash != "" || key == "" {
		t.Fatalf("unexpected device response: %+v key=%q", device, key)
	}
	if _, _, err := store.Redeem(token, "Second phone", now.Add(2*time.Minute)); err == nil {
		t.Fatal("one-time enrollment token was accepted twice")
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) || strings.Contains(string(data), key) {
		t.Fatal("mobile secret was persisted in plaintext")
	}
	reopened, err := OpenMobileAccessStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Authorize(key, now.Add(3*time.Minute)); !ok {
		t.Fatal("issued device credential was rejected")
	}
	if err := reopened.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Authorize(key, now.Add(4*time.Minute)); ok {
		t.Fatal("revoked device credential remained valid")
	}
}

func TestExpiredMobileEnrollmentIsRejected(t *testing.T) {
	store, err := OpenMobileAccessStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	enrollmentURL, _, _, err := store.CreateEnrollment("unit-1", "http://10.0.0.4:8787", now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(enrollmentURL)
	if _, _, err := store.Redeem(parsed.Query().Get("enroll"), "Late phone", now.Add(11*time.Minute)); err == nil {
		t.Fatal("expired enrollment token was accepted")
	}
}

func TestMobileDeviceCanReadScopedStateAndUpdateOnlyItsResponder(t *testing.T) {
	dataDir := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	configurator, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configurator.Update(NetworkSettingsInput{Mode: networkModeHost, ListenPort: 8787}); err != nil {
		t.Fatal(err)
	}
	network, err := OpenNetworkRuntime(dataDir, 0, logger)
	if err != nil {
		t.Fatal(err)
	}
	operational, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	responder, err := operational.CreateResponder(ResponderInput{Name: "Field Team", Status: "available", MapLabel: "FT", MarkerColor: "#123abc"})
	if err != nil {
		t.Fatal(err)
	}
	mobile, err := OpenMobileAccessStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	enrollmentURL, _, _, err := mobile.CreateEnrollment(responder.ID, "http://192.168.1.50:8787", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(enrollmentURL)
	apiServer, err := newAPIServer(operational, webAssets, logger)
	if err != nil {
		t.Fatal(err)
	}
	apiServer.network = network
	apiServer.mobile = mobile
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	enrollBody := `{"token":"` + parsed.Query().Get("enroll") + `","device_name":"Test phone"}`
	response, err := http.Post(server.URL+"/api/mobile/enroll", "application/json", strings.NewReader(enrollBody))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var enrolled struct {
		DeviceKey string `json:"device_key"`
	}
	if err := json.NewDecoder(response.Body).Decode(&enrolled); err != nil || response.StatusCode != http.StatusCreated || enrolled.DeviceKey == "" {
		t.Fatalf("enrollment status=%d response=%+v err=%v", response.StatusCode, enrolled, err)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/mobile/state", nil)
	request.Header.Set(mobileDeviceHeader, enrolled.DeviceKey)
	stateResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer stateResponse.Body.Close()
	var mobileState mobileStateResponse
	if err := json.NewDecoder(stateResponse.Body).Decode(&mobileState); err != nil || stateResponse.StatusCode != http.StatusOK {
		t.Fatalf("state status=%d err=%v", stateResponse.StatusCode, err)
	}
	if mobileState.Responder.ID != responder.ID {
		t.Fatalf("mobile state exposed wrong responder: %+v", mobileState.Responder)
	}

	statusBody := `{"status":"enroute","expected_updated_at":"` + responder.UpdatedAt.Format(time.RFC3339Nano) + `"}`
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/api/mobile/status", strings.NewReader(statusBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mobileDeviceHeader, enrolled.DeviceKey)
	statusResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResponse.Body.Close()
	if statusResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(statusResponse.Body)
		t.Fatalf("status update = %d, body=%s", statusResponse.StatusCode, body)
	}
	updated, _ := responderFromState(operational.Snapshot(), responder.ID)
	if updated.Status != "enroute" {
		t.Fatalf("responder status = %q, want enroute", updated.Status)
	}
	if updated.MapLabel != "FT" || updated.MarkerColor != "#123abc" {
		t.Fatalf("mobile status update cleared map appearance: %+v", updated)
	}

	locationBody := `{"latitude":35.828,"longitude":-83.575333,"accuracy_meters":12,"expected_updated_at":"` + updated.UpdatedAt.Format(time.RFC3339Nano) + `"}`
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/api/mobile/location", strings.NewReader(locationBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mobileDeviceHeader, enrolled.DeviceKey)
	locationResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer locationResponse.Body.Close()
	if locationResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(locationResponse.Body)
		t.Fatalf("location update = %d, body=%s", locationResponse.StatusCode, body)
	}
	updated, _ = responderFromState(operational.Snapshot(), responder.ID)
	if updated.Latitude == nil || updated.Longitude == nil || *updated.Latitude != 35.828 || *updated.Longitude != -83.575333 || updated.PositionSource != "device" {
		t.Fatalf("mobile location was not applied to the bound responder: %+v", updated)
	}

	request, _ = http.NewRequest(http.MethodPut, server.URL+"/api/mobile/location", strings.NewReader(`{"latitude":36,"longitude":-84}`))
	request.Header.Set("Content-Type", "application/json")
	unauthorizedResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer unauthorizedResponse.Body.Close()
	if unauthorizedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized location update = %d, want %d", unauthorizedResponse.StatusCode, http.StatusUnauthorized)
	}
}
