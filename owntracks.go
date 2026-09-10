package main

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
)

type ownTracksLocation struct {
	Type      string   `json:"_type"`
	Latitude  float64  `json:"lat"`
	Longitude float64  `json:"lon"`
	Timestamp int64    `json:"tst"`
	Accuracy  *float64 `json:"acc,omitempty"`
	Velocity  *float64 `json:"vel,omitempty"`
	Course    *float64 `json:"cog,omitempty"`
	Altitude  *float64 `json:"alt,omitempty"`
	TrackerID string   `json:"tid,omitempty"`
}

func (s *apiServer) ownTracks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tracking/owntracks/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	var input ownTracksLocation
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Type != "location" {
		writeError(w, http.StatusBadRequest, "OwnTracks payload must be a location")
		return
	}
	if input.Accuracy != nil && (*input.Accuracy < 0 || *input.Accuracy > 100000) {
		writeError(w, http.StatusBadRequest, "OwnTracks accuracy is invalid")
		return
	}
	receivedAt := time.Now().UTC()
	if input.Timestamp > 0 {
		candidate := time.Unix(input.Timestamp, 0).UTC()
		validated, validateErr := validateTrackingTime(candidate, receivedAt, "OwnTracks")
		if validateErr != nil {
			writeError(w, http.StatusBadRequest, validateErr.Error())
			return
		}
		receivedAt = validated
	}
	var speedKnots, altitudeFeet *float64
	if input.Velocity != nil {
		value := *input.Velocity / 1.852
		speedKnots = &value
	}
	if input.Altitude != nil {
		value := *input.Altitude * 3.28084
		altitudeFeet = &value
	}
	if input.Course != nil && (math.IsNaN(*input.Course) || *input.Course < 0 || *input.Course > 360) {
		writeError(w, http.StatusBadRequest, "OwnTracks course is invalid")
		return
	}
	responder, accepted, err := s.store.RecordExternalPosition(id, "owntracks", input.Latitude, input.Longitude, speedKnots, input.Course, altitudeFeet, receivedAt)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !accepted {
		writeError(w, http.StatusConflict, "OwnTracks position is older than the responder's current position")
		return
	}
	writeJSON(w, http.StatusOK, []any{})
	_ = responder
}

func validateExternalPosition(latitude, longitude float64, source string) error {
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 || math.IsNaN(latitude) || math.IsNaN(longitude) {
		return validationError{"external coordinates are invalid"}
	}
	if source != "owntracks" && source != "opengts" {
		return errors.New("unsupported external position source")
	}
	return nil
}
