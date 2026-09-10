package main

import (
	"net/http"
	"strings"
	"time"
)

type mobileStateResponse struct {
	Responder Responder  `json:"responder"`
	Incidents []Incident `json:"incidents"`
}

func (s *apiServer) mobileAdminEnrollment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.mobileHostAvailable(w) {
		return
	}
	var input struct {
		ResponderID string `json:"responder_id"`
		HostURL     string `json:"host_url"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	status := s.network.Status()
	validHostURL := false
	for _, candidate := range status.HostURLs {
		if strings.TrimRight(candidate, "/") == strings.TrimRight(input.HostURL, "/") {
			validHostURL = true
			break
		}
	}
	if !validHostURL {
		writeError(w, http.StatusBadRequest, "choose one of this Host's current LAN addresses")
		return
	}
	snapshot := s.store.Snapshot()
	found := false
	for _, responder := range snapshot.Responders {
		if responder.ID == input.ResponderID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "responder not found")
		return
	}
	enrollmentURL, expiresAt, image, err := s.mobile.CreateEnrollment(input.ResponderID, input.HostURL, time.Now().UTC())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": enrollmentURL, "expires_at": expiresAt, "qr_data_uri": image})
}

func (s *apiServer) mobileAdminDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.mobileHostAvailable(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.mobile.Devices()})
}

func (s *apiServer) mobileAdminDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	if !s.mobileHostAvailable(w) {
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mobile/admin/devices/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if err := s.mobile.Revoke(id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *apiServer) mobileEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.mobileHostAvailable(w) {
		return
	}
	var input struct {
		Token      string `json:"token"`
		DeviceName string `json:"device_name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	device, key, err := s.mobile.Redeem(strings.TrimSpace(input.Token), input.DeviceName, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"device": device, "device_key": key})
}

func (s *apiServer) mobileState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	device, ok := s.authorizeMobileDevice(w, r)
	if !ok {
		return
	}
	snapshot := s.store.Snapshot()
	responder, ok := responderFromState(snapshot, device.ResponderID)
	if !ok {
		writeError(w, http.StatusGone, "the enrolled responder no longer exists")
		return
	}
	incidents := make([]Incident, 0)
	for _, incident := range snapshot.Incidents {
		if incident.Status != "closed" && incident.Status != "cancelled" {
			incidents = append(incidents, incident)
		}
	}
	writeJSON(w, http.StatusOK, mobileStateResponse{Responder: responder, Incidents: incidents})
}

func (s *apiServer) mobileStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	device, ok := s.authorizeMobileDevice(w, r)
	if !ok {
		return
	}
	var input struct {
		Status            string     `json:"status"`
		ExpectedUpdatedAt *time.Time `json:"expected_updated_at"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !oneOf(input.Status, "available", "assigned", "enroute", "onscene", "out_of_service") {
		writeError(w, http.StatusBadRequest, "invalid responder status")
		return
	}
	snapshot := s.store.Snapshot()
	responder, ok := responderFromState(snapshot, device.ResponderID)
	if !ok {
		writeError(w, http.StatusGone, "the enrolled responder no longer exists")
		return
	}
	updated, err := s.store.UpdateResponder(responder.ID, ResponderInput{
		Name: responder.Name, Callsign: responder.Callsign, Type: responder.Type, Status: input.Status,
		Phone: responder.Phone, Capabilities: responder.Capabilities, Latitude: responder.Latitude,
		Longitude: responder.Longitude, APRSEnabled: responder.APRSEnabled, Notes: responder.Notes,
		ExpectedUpdatedAt: input.ExpectedUpdatedAt,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.aprs != nil {
		s.aprs.Notify()
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *apiServer) authorizeMobileDevice(w http.ResponseWriter, r *http.Request) (MobileDevice, bool) {
	if !s.mobileHostAvailable(w) {
		return MobileDevice{}, false
	}
	device, ok := s.mobile.Authorize(strings.TrimSpace(r.Header.Get(mobileDeviceHeader)), time.Now().UTC())
	if !ok {
		writeError(w, http.StatusUnauthorized, "valid mobile device credential required")
		return MobileDevice{}, false
	}
	return device, true
}

func (s *apiServer) mobileHostAvailable(w http.ResponseWriter) bool {
	if s.mobile == nil || s.network == nil || s.network.Active().Mode != networkModeHost {
		writeError(w, http.StatusServiceUnavailable, "mobile enrollment requires active Host mode")
		return false
	}
	return true
}

func responderFromState(state State, id string) (Responder, bool) {
	for _, responder := range state.Responders {
		if responder.ID == id {
			return responder, true
		}
	}
	return Responder{}, false
}
