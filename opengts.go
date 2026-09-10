package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *apiServer) openGTS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tracking/opengts/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "OpenGTS form payload is invalid")
			return
		}
	}
	latitude, err := requiredFloat(r, "lat")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	longitude, err := requiredFloat(r, "lon")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	receivedAt, err := openGTSTime(r.FormValue("date"), r.FormValue("time"), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	speedKPH, err := optionalFormFloat(r, "speed", 0, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	heading, err := optionalFormFloat(r, "head", 0, 360)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	altitudeMeters, err := optionalFormFloat(r, "alt", -1000, 100000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var speedKnots, altitudeFeet *float64
	if speedKPH != nil {
		value := *speedKPH / 1.852
		speedKnots = &value
	}
	if altitudeMeters != nil {
		value := *altitudeMeters * 3.28084
		altitudeFeet = &value
	}
	_, accepted, err := s.store.RecordExternalPosition(id, "opengts", latitude, longitude, speedKnots, heading, altitudeFeet, receivedAt)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !accepted {
		writeError(w, http.StatusConflict, "OpenGTS position is older than the responder's current position")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK\n"))
}

func requiredFloat(r *http.Request, name string) (float64, error) {
	value := strings.TrimSpace(r.FormValue(name))
	parsed, err := strconv.ParseFloat(value, 64)
	if value == "" || err != nil {
		return 0, fmt.Errorf("OpenGTS %s is required and must be numeric", name)
	}
	return parsed, nil
}

func optionalFormFloat(r *http.Request, name string, minimum, maximum float64) (*float64, error) {
	value := strings.TrimSpace(r.FormValue(name))
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return nil, fmt.Errorf("OpenGTS %s is invalid", name)
	}
	return &parsed, nil
}

func openGTSTime(dateValue, timeValue string, now time.Time) (time.Time, error) {
	dateValue, timeValue = strings.TrimSpace(dateValue), strings.TrimSpace(timeValue)
	if dateValue == "" && timeValue == "" {
		return now, nil
	}
	if epoch, err := strconv.ParseInt(dateValue, 10, 64); err == nil && len(dateValue) >= 9 {
		return validateTrackingTime(time.Unix(epoch, 0).UTC(), now, "OpenGTS")
	}
	if len(timeValue) == 4 {
		timeValue += "00"
	}
	for _, layout := range []string{"20060102 150405", "060102 150405"} {
		if parsed, err := time.ParseInLocation(layout, dateValue+" "+timeValue, time.UTC); err == nil {
			return validateTrackingTime(parsed, now, "OpenGTS")
		}
	}
	return time.Time{}, fmt.Errorf("OpenGTS date/time must be epoch seconds or YYYYMMDD with HHMMSS UTC")
}

func validateTrackingTime(value, now time.Time, source string) (time.Time, error) {
	if value.Before(now.Add(-31*24*time.Hour)) || value.After(now.Add(10*time.Minute)) {
		return time.Time{}, fmt.Errorf("%s timestamp is outside the accepted window", source)
	}
	return value, nil
}
