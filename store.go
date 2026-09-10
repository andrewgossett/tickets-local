package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("record changed on another computer")

type validationError struct {
	message string
}

func (e validationError) Error() string { return e.message }

type Event struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	CreatedAt time.Time       `json:"created_at"`
	Change    json.RawMessage `json:"change"`
}

type changeSet struct {
	Incident               *Incident           `json:"incident,omitempty"`
	Responder              *Responder          `json:"responder,omitempty"`
	Responders             []Responder         `json:"responders,omitempty"`
	Facility               *Facility           `json:"facility,omitempty"`
	Location               *Location           `json:"location,omitempty"`
	LocationDeleteID       string              `json:"location_delete_id,omitempty"`
	Overlay                *MapOverlay         `json:"overlay,omitempty"`
	OverlayDeleteID        string              `json:"overlay_delete_id,omitempty"`
	Asset                  *Asset              `json:"asset,omitempty"`
	AssetDeleteID          string              `json:"asset_delete_id,omitempty"`
	ScheduleItem           *ScheduleItem       `json:"schedule_item,omitempty"`
	ScheduleDeleteID       string              `json:"schedule_delete_id,omitempty"`
	Qualification          *Qualification      `json:"qualification,omitempty"`
	QualificationDeleteID  string              `json:"qualification_delete_id,omitempty"`
	Message                *OperationalMessage `json:"message,omitempty"`
	Settings               *Settings           `json:"settings,omitempty"`
	Track                  *TrackPoint         `json:"track,omitempty"`
	TrackDeleteResponderID string              `json:"track_delete_responder_id,omitempty"`
	Activity               *Activity           `json:"activity,omitempty"`
}

type Store struct {
	mu               sync.RWMutex
	state            State
	dataDir          string
	eventPath        string
	aprsPasscodePath string
	subscribers      map[chan Event]struct{}
}

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	s := &Store{
		state: State{
			SchemaVersion: 3,
			Settings: Settings{
				Organization:         productName,
				CenterLat:            39.8283,
				CenterLon:            -98.5795,
				DefaultZoom:          4,
				IncidentStaleMinutes: 10,
				Weather: WeatherSettings{
					RefreshMinutes: 5,
					RadarOpacity:   55,
					RadarFrames:    4,
				},
				Water:        WaterSettings{RefreshMinutes: 15},
				Integrations: IntegrationSettings{RefreshMinutes: 5},
				APRS: APRSSettings{
					Mode:            "internet",
					Server:          "rotate.aprs2.net:14580",
					StaleMinutes:    15,
					TrailHours:      12,
					AreaRadiusMiles: 25,
					Local: APRSLocalSettings{
						Decoder:     "bundled",
						KISSAddress: "127.0.0.1:8001",
						ShowAll:     true,
					},
				},
			},
			Incidents:      []Incident{},
			Responders:     []Responder{},
			Facilities:     []Facility{},
			Locations:      []Location{},
			Overlays:       []MapOverlay{},
			Assets:         []Asset{},
			Schedule:       []ScheduleItem{},
			Qualifications: []Qualification{},
			Messages:       []OperationalMessage{},
			Tracks:         []TrackPoint{},
			APRSStations:   []APRSStation{},
			Activity:       []Activity{},
		},
		dataDir:          dataDir,
		eventPath:        filepath.Join(dataDir, "events.ndjson"),
		aprsPasscodePath: filepath.Join(dataDir, "aprs-passcode"),
		subscribers:      make(map[chan Event]struct{}),
	}

	file, err := os.Open(s.eventPath)
	if errors.Is(err, os.ErrNotExist) {
		s.applySettingDefaults()
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("read event log line %d: %w", line, err)
		}
		if err := s.applyEvent(event); err != nil {
			return nil, fmt.Errorf("apply event log line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan event log: %w", err)
	}
	s.applySettingDefaults()
	s.trimTracks(time.Now().UTC())
	return s, nil
}

func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, _ := json.Marshal(s.state)
	var snapshot State
	_ = json.Unmarshal(data, &snapshot)
	return snapshot
}

func (s *Store) APRSPasscode() string {
	data, err := os.ReadFile(s.aprsPasscodePath)
	if err != nil {
		return ""
	}
	passcode := strings.TrimSpace(string(data))
	if !validAPRSPasscode(passcode) {
		return ""
	}
	return passcode
}

func (s *Store) SaveAPRSPasscode(passcode string) error {
	passcode = strings.TrimSpace(passcode)
	if !validAPRSPasscode(passcode) {
		return validationError{"APRS-IS passcode must contain one to five digits"}
	}
	if err := os.WriteFile(s.aprsPasscodePath, []byte(passcode+"\n"), 0o600); err != nil {
		return fmt.Errorf("save APRS-IS passcode: %w", err)
	}
	if err := os.Chmod(s.aprsPasscodePath, 0o600); err != nil {
		return fmt.Errorf("protect APRS-IS passcode: %w", err)
	}
	s.mu.Lock()
	s.state.Settings.APRS.PasscodeConfigured = true
	s.mu.Unlock()
	return nil
}

func (s *Store) EventLog(w io.Writer) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	file, err := os.Open(s.eventPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(w, file)
	return err
}

func (s *Store) Subscribe() (<-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan Event, 16)
	s.subscribers[ch] = struct{}{}
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
	}
}

func (s *Store) CreateIncident(input IncidentInput) (Incident, error) {
	if err := validateIncidentInput(input, false); err != nil {
		return Incident{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	command, err := buildCommandRoles(nil, input.Command, s.state.Responders, time.Now().UTC())
	if err != nil {
		return Incident{}, err
	}

	now := time.Now().UTC()
	status := input.Status
	if status == "" {
		status = "new"
	}
	incident := Incident{
		ID:           newID("inc"),
		Number:       s.nextIncidentNumber(),
		Title:        clean(input.Title),
		Type:         clean(input.Type),
		Severity:     input.Severity,
		Status:       status,
		Address:      clean(input.Address),
		City:         clean(input.City),
		Region:       clean(input.Region),
		PostalCode:   clean(input.PostalCode),
		Latitude:     input.Latitude,
		Longitude:    input.Longitude,
		ContactName:  clean(input.ContactName),
		ContactPhone: clean(input.ContactPhone),
		Description:  strings.TrimSpace(input.Description),
		Assignments:  []Assignment{},
		Actions:      []Action{},
		Attachments:  []IncidentAttachment{},
		Command:      command,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if status == "closed" || status == "cancelled" {
		incident.ClosedAt = &now
	}
	activity := newActivity("incident.created", fmt.Sprintf("Incident #%d created: %s", incident.Number, incident.Title), "incident", incident.ID)
	if _, err := s.commitLocked("incident.created", changeSet{Incident: &incident, Activity: &activity}); err != nil {
		return Incident{}, err
	}
	return incident, nil
}

func (s *Store) AddIncidentAttachment(incidentID string, attachment IncidentAttachment) (Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findIncident(s.state.Incidents, incidentID)
	if index < 0 {
		return Incident{}, ErrNotFound
	}
	incident := s.state.Incidents[index]
	if len(incident.Attachments) >= 20 {
		return Incident{}, validationError{"an incident may have no more than 20 attachments"}
	}
	incident.Attachments = append(incident.Attachments, attachment)
	incident.UpdatedAt = time.Now().UTC()
	activity := newActivity("incident.attachment", fmt.Sprintf("Attachment added to incident #%d: %s", incident.Number, attachment.Name), "incident", incident.ID)
	if _, err := s.commitLocked("incident.attachment", changeSet{Incident: &incident, Activity: &activity}); err != nil {
		return Incident{}, err
	}
	return incident, nil
}

func (s *Store) UpdateIncident(id string, input IncidentInput) (Incident, error) {
	if err := validateIncidentInput(input, true); err != nil {
		return Incident{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index := findIncident(s.state.Incidents, id)
	if index < 0 {
		return Incident{}, ErrNotFound
	}
	incident := s.state.Incidents[index]
	if staleUpdate(input.ExpectedUpdatedAt, incident.UpdatedAt) {
		return Incident{}, ErrConflict
	}
	oldStatus := incident.Status
	now := time.Now().UTC()
	if input.Command != nil {
		command, err := buildCommandRoles(incident.Command, input.Command, s.state.Responders, now)
		if err != nil {
			return Incident{}, err
		}
		incident.Command = command
	}
	incident.Title = clean(input.Title)
	incident.Type = clean(input.Type)
	incident.Severity = input.Severity
	incident.Status = input.Status
	incident.Address = clean(input.Address)
	incident.City = clean(input.City)
	incident.Region = clean(input.Region)
	incident.PostalCode = clean(input.PostalCode)
	incident.Latitude = input.Latitude
	incident.Longitude = input.Longitude
	incident.ContactName = clean(input.ContactName)
	incident.ContactPhone = clean(input.ContactPhone)
	incident.Description = strings.TrimSpace(input.Description)
	incident.UpdatedAt = now
	if input.Status == "closed" || input.Status == "cancelled" {
		if incident.ClosedAt == nil {
			incident.ClosedAt = &now
		}
	} else {
		incident.ClosedAt = nil
	}

	summary := fmt.Sprintf("Incident #%d updated", incident.Number)
	kind := "incident.updated"
	if oldStatus != input.Status {
		kind = "incident.status"
		summary = fmt.Sprintf("Incident #%d status changed: %s → %s", incident.Number, label(oldStatus), label(input.Status))
	}
	released := []Responder{}
	if activeIncidentStatus(oldStatus) && !activeIncidentStatus(input.Status) {
		for _, assignment := range incident.Assignments {
			responderIndex := findResponder(s.state.Responders, assignment.ResponderID)
			if responderIndex < 0 {
				continue
			}
			responder := s.state.Responders[responderIndex]
			if responder.Status != "out_of_service" {
				responder.Status = "available"
				responder.UpdatedAt = now
				released = append(released, responder)
			}
		}
		incident.Assignments = []Assignment{}
	}
	activity := newActivity(kind, summary, "incident", incident.ID)
	if _, err := s.commitLocked(kind, changeSet{Incident: &incident, Responders: released, Activity: &activity}); err != nil {
		return Incident{}, err
	}
	return incident, nil
}

func (s *Store) AddIncidentAction(id, description string) (Incident, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return Incident{}, validationError{"action description is required"}
	}
	if len(description) > 4000 {
		return Incident{}, validationError{"action description must be 4,000 characters or fewer"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	index := findIncident(s.state.Incidents, id)
	if index < 0 {
		return Incident{}, ErrNotFound
	}
	incident := s.state.Incidents[index]
	now := time.Now().UTC()
	incident.Actions = append(incident.Actions, Action{
		ID:          newID("act"),
		Description: description,
		CreatedAt:   now,
	})
	incident.UpdatedAt = now
	activity := newActivity("incident.action", fmt.Sprintf("Incident #%d: %s", incident.Number, abbreviate(description, 100)), "incident", incident.ID)
	if _, err := s.commitLocked("incident.action", changeSet{Incident: &incident, Activity: &activity}); err != nil {
		return Incident{}, err
	}
	return incident, nil
}

func (s *Store) AssignResponder(incidentID, responderID string) (Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	incidentIndex := findIncident(s.state.Incidents, incidentID)
	if incidentIndex < 0 {
		return Incident{}, ErrNotFound
	}
	responderIndex := findResponder(s.state.Responders, responderID)
	if responderIndex < 0 {
		return Incident{}, validationError{"selected responder no longer exists"}
	}
	incident := s.state.Incidents[incidentIndex]
	if slices.ContainsFunc(incident.Assignments, func(item Assignment) bool { return item.ResponderID == responderID }) {
		return incident, nil
	}
	if incident.Status == "closed" || incident.Status == "cancelled" {
		return Incident{}, validationError{"closed or cancelled incidents cannot receive assignments"}
	}
	responder := s.state.Responders[responderIndex]
	if responder.Status != "available" {
		return Incident{}, validationError{fmt.Sprintf("%s is not available for assignment", responderDisplayName(responder))}
	}

	now := time.Now().UTC()
	incident.Assignments = append(incident.Assignments, Assignment{ResponderID: responderID, AssignedAt: now})
	if incident.Status == "new" {
		incident.Status = "assigned"
	}
	incident.UpdatedAt = now

	responder.Status = "assigned"
	responder.UpdatedAt = now
	activity := newActivity(
		"incident.assigned",
		fmt.Sprintf("%s assigned to incident #%d", responderDisplayName(responder), incident.Number),
		"incident",
		incident.ID,
	)
	if _, err := s.commitLocked("incident.assigned", changeSet{Incident: &incident, Responder: &responder, Activity: &activity}); err != nil {
		return Incident{}, err
	}
	return incident, nil
}

func (s *Store) UnassignResponder(incidentID, responderID string) (Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	incidentIndex := findIncident(s.state.Incidents, incidentID)
	if incidentIndex < 0 {
		return Incident{}, ErrNotFound
	}
	incident := s.state.Incidents[incidentIndex]
	before := len(incident.Assignments)
	incident.Assignments = slices.DeleteFunc(incident.Assignments, func(item Assignment) bool {
		return item.ResponderID == responderID
	})
	if len(incident.Assignments) == before {
		return incident, nil
	}
	now := time.Now().UTC()
	incident.UpdatedAt = now
	if len(incident.Assignments) == 0 && incident.Status == "assigned" {
		incident.Status = "new"
	}

	var responder *Responder
	responderName := "Responder"
	if responderIndex := findResponder(s.state.Responders, responderID); responderIndex >= 0 {
		item := s.state.Responders[responderIndex]
		responderName = responderDisplayName(item)
		if item.Status == "assigned" {
			item.Status = "available"
			item.UpdatedAt = now
		}
		responder = &item
	}
	activity := newActivity("incident.unassigned", fmt.Sprintf("%s cleared from incident #%d", responderName, incident.Number), "incident", incident.ID)
	if _, err := s.commitLocked("incident.unassigned", changeSet{Incident: &incident, Responder: responder, Activity: &activity}); err != nil {
		return Incident{}, err
	}
	return incident, nil
}

func (s *Store) CreateResponder(input ResponderInput) (Responder, error) {
	if err := validateResponderInput(input); err != nil {
		return Responder{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	responder := Responder{
		ID:           newID("unit"),
		Name:         clean(input.Name),
		Callsign:     strings.ToUpper(clean(input.Callsign)),
		Type:         clean(input.Type),
		Status:       defaultString(input.Status, "available"),
		Phone:        clean(input.Phone),
		Capabilities: cleanList(input.Capabilities),
		Latitude:     input.Latitude,
		Longitude:    input.Longitude,
		APRSEnabled:  input.APRSEnabled,
		MapLabel:     normalizeMapLabel(input.MapLabel),
		MarkerColor:  normalizeMarkerColor(input.MarkerColor),
		Notes:        strings.TrimSpace(input.Notes),
		UpdatedAt:    now,
	}
	activity := newActivity("responder.created", fmt.Sprintf("Responder added: %s", responderDisplayName(responder)), "responder", responder.ID)
	if _, err := s.commitLocked("responder.created", changeSet{Responder: &responder, Activity: &activity}); err != nil {
		return Responder{}, err
	}
	return responder, nil
}

func (s *Store) UpdateResponder(id string, input ResponderInput) (Responder, error) {
	if err := validateResponderInput(input); err != nil {
		return Responder{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	index := findResponder(s.state.Responders, id)
	if index < 0 {
		return Responder{}, ErrNotFound
	}
	responder := s.state.Responders[index]
	if staleUpdate(input.ExpectedUpdatedAt, responder.UpdatedAt) {
		return Responder{}, ErrConflict
	}
	previousCallsign := responder.Callsign
	previousAPRSEnabled := responder.APRSEnabled
	responder.Name = clean(input.Name)
	responder.Callsign = strings.ToUpper(clean(input.Callsign))
	responder.Type = clean(input.Type)
	responder.Status = defaultString(input.Status, "available")
	responder.Phone = clean(input.Phone)
	responder.Capabilities = cleanList(input.Capabilities)
	responder.Latitude = input.Latitude
	responder.Longitude = input.Longitude
	responder.APRSEnabled = input.APRSEnabled
	responder.MapLabel = normalizeMapLabel(input.MapLabel)
	responder.MarkerColor = normalizeMarkerColor(input.MarkerColor)
	aprsIdentityChanged := input.APRSEnabled &&
		(!previousAPRSEnabled || !strings.EqualFold(previousCallsign, responder.Callsign))
	resetAPRSState := !input.APRSEnabled || aprsIdentityChanged
	if aprsIdentityChanged {
		responder.Latitude = nil
		responder.Longitude = nil
	}
	if resetAPRSState {
		responder.PositionSource = ""
		responder.PositionUpdatedAt = nil
		responder.SpeedKnots = nil
		responder.CourseDegrees = nil
		responder.AltitudeFeet = nil
		responder.APRSSymbol = ""
		responder.APRSComment = ""
	}
	responder.Notes = strings.TrimSpace(input.Notes)
	responder.UpdatedAt = time.Now().UTC()
	activity := newActivity("responder.updated", fmt.Sprintf("%s updated — %s", responderDisplayName(responder), label(responder.Status)), "responder", responder.ID)
	change := changeSet{Responder: &responder, Activity: &activity}
	if resetAPRSState {
		change.TrackDeleteResponderID = responder.ID
	}
	if _, err := s.commitLocked("responder.updated", change); err != nil {
		return Responder{}, err
	}
	return responder, nil
}

func (s *Store) UpdateResponderDevicePosition(id string, input ResponderPositionInput) (Responder, error) {
	if input.Latitude < -90 || input.Latitude > 90 || input.Longitude < -180 || input.Longitude > 180 {
		return Responder{}, validationError{"device coordinates are invalid"}
	}
	if input.AccuracyMeters != nil && (*input.AccuracyMeters < 0 || *input.AccuracyMeters > 100000) {
		return Responder{}, validationError{"device location accuracy is invalid"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findResponder(s.state.Responders, id)
	if index < 0 {
		return Responder{}, ErrNotFound
	}
	responder := s.state.Responders[index]
	if staleUpdate(input.ExpectedUpdatedAt, responder.UpdatedAt) {
		return Responder{}, ErrConflict
	}
	now := time.Now().UTC()
	responder.Latitude, responder.Longitude = &input.Latitude, &input.Longitude
	responder.PositionSource, responder.PositionUpdatedAt, responder.UpdatedAt = "device", &now, now
	responder.SpeedKnots, responder.CourseDegrees, responder.AltitudeFeet = nil, nil, nil
	activity := newActivity("responder.device_position", fmt.Sprintf("Device position updated: %s", responderDisplayName(responder)), "responder", responder.ID)
	if _, err := s.commitLocked("responder.device_position", changeSet{Responder: &responder, Activity: &activity}); err != nil {
		return Responder{}, err
	}
	return responder, nil
}

func (s *Store) RecordExternalPosition(id, source string, latitude, longitude float64, speedKnots, courseDegrees, altitudeFeet *float64, receivedAt time.Time) (Responder, bool, error) {
	if err := validateExternalPosition(latitude, longitude, source); err != nil {
		return Responder{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findResponder(s.state.Responders, id)
	if index < 0 {
		return Responder{}, false, ErrNotFound
	}
	responder := s.state.Responders[index]
	receivedAt = receivedAt.UTC()
	if responder.PositionUpdatedAt != nil && !receivedAt.After(*responder.PositionUpdatedAt) {
		return responder, false, nil
	}
	responder.Latitude, responder.Longitude = pointer(latitude), pointer(longitude)
	responder.PositionSource, responder.PositionUpdatedAt = source, &receivedAt
	responder.SpeedKnots, responder.CourseDegrees, responder.AltitudeFeet = speedKnots, courseDegrees, altitudeFeet
	track := TrackPoint{ID: newID("trk"), ResponderID: responder.ID, Callsign: responder.Callsign, Latitude: latitude, Longitude: longitude, SpeedKnots: speedKnots, CourseDegrees: courseDegrees, AltitudeFeet: altitudeFeet, Source: source, ReceivedAt: receivedAt}
	if _, err := s.commitLocked("tracking.position", changeSet{Responder: &responder, Track: &track}); err != nil {
		return Responder{}, false, err
	}
	s.trimTracks(receivedAt)
	return responder, true, nil
}

func (s *Store) CreateFacility(input FacilityInput) (Facility, error) {
	if err := validateFacilityInput(input); err != nil {
		return Facility{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	facility := Facility{
		ID:          newID("fac"),
		Name:        clean(input.Name),
		Type:        clean(input.Type),
		Status:      defaultString(input.Status, "open"),
		Address:     clean(input.Address),
		Phone:       clean(input.Phone),
		Capacity:    input.Capacity,
		Occupied:    input.Occupied,
		Latitude:    input.Latitude,
		Longitude:   input.Longitude,
		MapLabel:    normalizeMapLabel(input.MapLabel),
		MarkerColor: normalizeMarkerColor(input.MarkerColor),
		Notes:       strings.TrimSpace(input.Notes),
		UpdatedAt:   time.Now().UTC(),
	}
	activity := newActivity("facility.created", fmt.Sprintf("Facility added: %s", facility.Name), "facility", facility.ID)
	if _, err := s.commitLocked("facility.created", changeSet{Facility: &facility, Activity: &activity}); err != nil {
		return Facility{}, err
	}
	return facility, nil
}

func (s *Store) UpdateFacility(id string, input FacilityInput) (Facility, error) {
	if err := validateFacilityInput(input); err != nil {
		return Facility{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	index := findFacility(s.state.Facilities, id)
	if index < 0 {
		return Facility{}, ErrNotFound
	}
	facility := s.state.Facilities[index]
	if staleUpdate(input.ExpectedUpdatedAt, facility.UpdatedAt) {
		return Facility{}, ErrConflict
	}
	facility.Name = clean(input.Name)
	facility.Type = clean(input.Type)
	facility.Status = defaultString(input.Status, "open")
	facility.Address = clean(input.Address)
	facility.Phone = clean(input.Phone)
	facility.Capacity = input.Capacity
	facility.Occupied = input.Occupied
	facility.Latitude = input.Latitude
	facility.Longitude = input.Longitude
	facility.MapLabel = normalizeMapLabel(input.MapLabel)
	facility.MarkerColor = normalizeMarkerColor(input.MarkerColor)
	facility.Notes = strings.TrimSpace(input.Notes)
	facility.UpdatedAt = time.Now().UTC()
	activity := newActivity("facility.updated", fmt.Sprintf("%s updated — %s", facility.Name, label(facility.Status)), "facility", facility.ID)
	if _, err := s.commitLocked("facility.updated", changeSet{Facility: &facility, Activity: &activity}); err != nil {
		return Facility{}, err
	}
	return facility, nil
}

func (s *Store) CreateLocation(input LocationInput) (Location, error) {
	if err := validateLocationInput(input); err != nil {
		return Location{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	location := Location{
		ID:         newID("loc"),
		Name:       clean(input.Name),
		Type:       clean(input.Type),
		Address:    clean(input.Address),
		City:       clean(input.City),
		Region:     clean(input.Region),
		PostalCode: clean(input.PostalCode),
		Latitude:   input.Latitude,
		Longitude:  input.Longitude,
		Notes:      strings.TrimSpace(input.Notes),
		UpdatedAt:  time.Now().UTC(),
	}
	activity := newActivity("location.created", fmt.Sprintf("Location added: %s", location.Name), "location", location.ID)
	if _, err := s.commitLocked("location.created", changeSet{Location: &location, Activity: &activity}); err != nil {
		return Location{}, err
	}
	return location, nil
}

func (s *Store) UpdateLocation(id string, input LocationInput) (Location, error) {
	if err := validateLocationInput(input); err != nil {
		return Location{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	index := findLocation(s.state.Locations, id)
	if index < 0 {
		return Location{}, ErrNotFound
	}
	location := s.state.Locations[index]
	if staleUpdate(input.ExpectedUpdatedAt, location.UpdatedAt) {
		return Location{}, ErrConflict
	}
	location.Name = clean(input.Name)
	location.Type = clean(input.Type)
	location.Address = clean(input.Address)
	location.City = clean(input.City)
	location.Region = clean(input.Region)
	location.PostalCode = clean(input.PostalCode)
	location.Latitude = input.Latitude
	location.Longitude = input.Longitude
	location.Notes = strings.TrimSpace(input.Notes)
	location.UpdatedAt = time.Now().UTC()
	activity := newActivity("location.updated", fmt.Sprintf("Location updated: %s", location.Name), "location", location.ID)
	if _, err := s.commitLocked("location.updated", changeSet{Location: &location, Activity: &activity}); err != nil {
		return Location{}, err
	}
	return location, nil
}

func (s *Store) DeleteLocation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := findLocation(s.state.Locations, id)
	if index < 0 {
		return ErrNotFound
	}
	location := s.state.Locations[index]
	activity := newActivity("location.deleted", fmt.Sprintf("Location removed: %s", location.Name), "location", location.ID)
	_, err := s.commitLocked("location.deleted", changeSet{LocationDeleteID: id, Activity: &activity})
	return err
}

func (s *Store) CreateOverlay(input MapOverlayInput) (MapOverlay, error) {
	if err := validateOverlayInput(input); err != nil {
		return MapOverlay{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	overlay := MapOverlay{
		ID:        newID("ovl"),
		Name:      clean(input.Name),
		FileName:  clean(input.FileName),
		Color:     normalizeOverlayColor(input.Color),
		Visible:   input.Visible,
		Features:  input.Features,
		CreatedAt: now,
		UpdatedAt: now,
	}
	verb := "imported"
	if overlay.FileName == "Map drawing" {
		verb = "drawn"
	}
	activity := newActivity("overlay.created", fmt.Sprintf("Map overlay %s: %s", verb, overlay.Name), "overlay", overlay.ID)
	if _, err := s.commitLocked("overlay.created", changeSet{Overlay: &overlay, Activity: &activity}); err != nil {
		return MapOverlay{}, err
	}
	return overlay, nil
}

func (s *Store) CreateAsset(input AssetInput) (Asset, error) {
	if err := validateAssetInput(input); err != nil {
		return Asset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	asset := Asset{ID: newID("ast"), Category: input.Category, Name: clean(input.Name), Identifier: clean(input.Identifier), Status: defaultString(input.Status, "ready"), Quantity: input.Quantity, Location: clean(input.Location), Custodian: clean(input.Custodian), MaintenanceDue: input.MaintenanceDue, Notes: strings.TrimSpace(input.Notes), CreatedAt: now, UpdatedAt: now}
	activity := newActivity("asset.created", fmt.Sprintf("%s added: %s", label(asset.Category), asset.Name), "asset", asset.ID)
	if _, err := s.commitLocked("asset.created", changeSet{Asset: &asset, Activity: &activity}); err != nil {
		return Asset{}, err
	}
	return asset, nil
}

func (s *Store) UpdateAsset(id string, input AssetInput) (Asset, error) {
	if err := validateAssetInput(input); err != nil {
		return Asset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findAsset(s.state.Assets, id)
	if index < 0 {
		return Asset{}, ErrNotFound
	}
	asset := s.state.Assets[index]
	if staleUpdate(input.ExpectedUpdatedAt, asset.UpdatedAt) {
		return Asset{}, ErrConflict
	}
	asset.Category, asset.Name, asset.Identifier, asset.Status = input.Category, clean(input.Name), clean(input.Identifier), defaultString(input.Status, "ready")
	asset.Quantity, asset.Location, asset.Custodian = input.Quantity, clean(input.Location), clean(input.Custodian)
	asset.MaintenanceDue, asset.Notes, asset.UpdatedAt = input.MaintenanceDue, strings.TrimSpace(input.Notes), time.Now().UTC()
	activity := newActivity("asset.updated", fmt.Sprintf("%s updated: %s", label(asset.Category), asset.Name), "asset", asset.ID)
	if _, err := s.commitLocked("asset.updated", changeSet{Asset: &asset, Activity: &activity}); err != nil {
		return Asset{}, err
	}
	return asset, nil
}

func (s *Store) DeleteAsset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findAsset(s.state.Assets, id)
	if index < 0 {
		return ErrNotFound
	}
	asset := s.state.Assets[index]
	activity := newActivity("asset.deleted", fmt.Sprintf("%s removed: %s", label(asset.Category), asset.Name), "asset", asset.ID)
	_, err := s.commitLocked("asset.deleted", changeSet{AssetDeleteID: id, Activity: &activity})
	return err
}

func (s *Store) CreateScheduleItem(input ScheduleItemInput) (ScheduleItem, error) {
	if err := validateScheduleItemInput(input); err != nil {
		return ScheduleItem{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scheduleRespondersExist(s.state.Responders, input.AssignedResponderIDs) {
		return ScheduleItem{}, validationError{"one or more assigned responders do not exist"}
	}
	now := time.Now().UTC()
	item := ScheduleItem{ID: newID("sch"), Title: clean(input.Title), Type: input.Type, Status: defaultString(input.Status, "planned"), StartAt: input.StartAt.UTC(), EndAt: input.EndAt.UTC(), Location: clean(input.Location), AssignedResponderIDs: cleanList(input.AssignedResponderIDs), Coordinator: clean(input.Coordinator), Notes: strings.TrimSpace(input.Notes), CreatedAt: now, UpdatedAt: now}
	activity := newActivity("schedule.created", fmt.Sprintf("Scheduled %s: %s", label(item.Type), item.Title), "schedule", item.ID)
	if _, err := s.commitLocked("schedule.created", changeSet{ScheduleItem: &item, Activity: &activity}); err != nil {
		return ScheduleItem{}, err
	}
	return item, nil
}

func (s *Store) UpdateScheduleItem(id string, input ScheduleItemInput) (ScheduleItem, error) {
	if err := validateScheduleItemInput(input); err != nil {
		return ScheduleItem{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findScheduleItem(s.state.Schedule, id)
	if index < 0 {
		return ScheduleItem{}, ErrNotFound
	}
	item := s.state.Schedule[index]
	if staleUpdate(input.ExpectedUpdatedAt, item.UpdatedAt) {
		return ScheduleItem{}, ErrConflict
	}
	if !scheduleRespondersExist(s.state.Responders, input.AssignedResponderIDs) {
		return ScheduleItem{}, validationError{"one or more assigned responders do not exist"}
	}
	item.Title, item.Type, item.Status = clean(input.Title), input.Type, defaultString(input.Status, "planned")
	item.StartAt, item.EndAt, item.Location = input.StartAt.UTC(), input.EndAt.UTC(), clean(input.Location)
	item.AssignedResponderIDs, item.Coordinator, item.Notes = cleanList(input.AssignedResponderIDs), clean(input.Coordinator), strings.TrimSpace(input.Notes)
	item.UpdatedAt = time.Now().UTC()
	activity := newActivity("schedule.updated", fmt.Sprintf("Schedule updated: %s", item.Title), "schedule", item.ID)
	if _, err := s.commitLocked("schedule.updated", changeSet{ScheduleItem: &item, Activity: &activity}); err != nil {
		return ScheduleItem{}, err
	}
	return item, nil
}

func (s *Store) DeleteScheduleItem(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findScheduleItem(s.state.Schedule, id)
	if index < 0 {
		return ErrNotFound
	}
	item := s.state.Schedule[index]
	activity := newActivity("schedule.deleted", fmt.Sprintf("Schedule removed: %s", item.Title), "schedule", item.ID)
	_, err := s.commitLocked("schedule.deleted", changeSet{ScheduleDeleteID: id, Activity: &activity})
	return err
}

func (s *Store) CreateQualification(input QualificationInput) (Qualification, error) {
	if err := validateQualificationInput(input); err != nil {
		return Qualification{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if findResponder(s.state.Responders, input.ResponderID) < 0 {
		return Qualification{}, validationError{"selected responder does not exist"}
	}
	now := time.Now().UTC()
	item := Qualification{ID: newID("qual"), ResponderID: input.ResponderID, Category: input.Category, Name: clean(input.Name), Provider: clean(input.Provider), CredentialID: clean(input.CredentialID), Status: defaultString(input.Status, "current"), CompletedAt: utcTimePointer(input.CompletedAt), ExpiresAt: utcTimePointer(input.ExpiresAt), Notes: strings.TrimSpace(input.Notes), CreatedAt: now, UpdatedAt: now}
	activity := newActivity("qualification.created", fmt.Sprintf("Qualification added: %s", item.Name), "qualification", item.ID)
	if _, err := s.commitLocked("qualification.created", changeSet{Qualification: &item, Activity: &activity}); err != nil {
		return Qualification{}, err
	}
	return item, nil
}

func (s *Store) UpdateQualification(id string, input QualificationInput) (Qualification, error) {
	if err := validateQualificationInput(input); err != nil {
		return Qualification{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findQualification(s.state.Qualifications, id)
	if index < 0 {
		return Qualification{}, ErrNotFound
	}
	item := s.state.Qualifications[index]
	if staleUpdate(input.ExpectedUpdatedAt, item.UpdatedAt) {
		return Qualification{}, ErrConflict
	}
	if findResponder(s.state.Responders, input.ResponderID) < 0 {
		return Qualification{}, validationError{"selected responder does not exist"}
	}
	item.ResponderID, item.Category, item.Name = input.ResponderID, input.Category, clean(input.Name)
	item.Provider, item.CredentialID, item.Status = clean(input.Provider), clean(input.CredentialID), defaultString(input.Status, "current")
	item.CompletedAt, item.ExpiresAt, item.Notes, item.UpdatedAt = utcTimePointer(input.CompletedAt), utcTimePointer(input.ExpiresAt), strings.TrimSpace(input.Notes), time.Now().UTC()
	activity := newActivity("qualification.updated", fmt.Sprintf("Qualification updated: %s", item.Name), "qualification", item.ID)
	if _, err := s.commitLocked("qualification.updated", changeSet{Qualification: &item, Activity: &activity}); err != nil {
		return Qualification{}, err
	}
	return item, nil
}

func (s *Store) DeleteQualification(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := findQualification(s.state.Qualifications, id)
	if index < 0 {
		return ErrNotFound
	}
	item := s.state.Qualifications[index]
	activity := newActivity("qualification.deleted", fmt.Sprintf("Qualification removed: %s", item.Name), "qualification", item.ID)
	_, err := s.commitLocked("qualification.deleted", changeSet{QualificationDeleteID: id, Activity: &activity})
	return err
}

func (s *Store) CreateOperationalMessage(input OperationalMessageInput) (OperationalMessage, error) {
	if err := validateOperationalMessageInput(input); err != nil {
		return OperationalMessage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.IncidentID != "" && findIncident(s.state.Incidents, input.IncidentID) < 0 {
		return OperationalMessage{}, validationError{"selected incident does not exist"}
	}
	message := OperationalMessage{ID: newID("msg"), Channel: defaultString(input.Channel, "general"), IncidentID: clean(input.IncidentID), Author: clean(input.Author), Body: strings.TrimSpace(input.Body), CreatedAt: time.Now().UTC()}
	activity := newActivity("message.sent", fmt.Sprintf("Message posted to %s", label(message.Channel)), "message", message.ID)
	if _, err := s.commitLocked("message.sent", changeSet{Message: &message, Activity: &activity}); err != nil {
		return OperationalMessage{}, err
	}
	return message, nil
}

func (s *Store) UpdateOverlay(id string, input MapOverlayUpdateInput) (MapOverlay, error) {
	if err := validateOverlayUpdateInput(input); err != nil {
		return MapOverlay{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	index := findOverlay(s.state.Overlays, id)
	if index < 0 {
		return MapOverlay{}, ErrNotFound
	}
	overlay := s.state.Overlays[index]
	if staleUpdate(input.ExpectedUpdatedAt, overlay.UpdatedAt) {
		return MapOverlay{}, ErrConflict
	}
	overlay.Name = clean(input.Name)
	overlay.Color = normalizeOverlayColor(input.Color)
	overlay.Visible = input.Visible
	overlay.UpdatedAt = time.Now().UTC()
	activity := newActivity("overlay.updated", fmt.Sprintf("Map overlay updated: %s", overlay.Name), "overlay", overlay.ID)
	if _, err := s.commitLocked("overlay.updated", changeSet{Overlay: &overlay, Activity: &activity}); err != nil {
		return MapOverlay{}, err
	}
	return overlay, nil
}

func (s *Store) DeleteOverlay(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := findOverlay(s.state.Overlays, id)
	if index < 0 {
		return ErrNotFound
	}
	overlay := s.state.Overlays[index]
	activity := newActivity("overlay.deleted", fmt.Sprintf("Map overlay removed: %s", overlay.Name), "overlay", overlay.ID)
	_, err := s.commitLocked("overlay.deleted", changeSet{OverlayDeleteID: id, Activity: &activity})
	return err
}

func (s *Store) UpdateSettings(settings Settings) (Settings, error) {
	settings.Organization = clean(settings.Organization)
	settings.CenterAddress = clean(settings.CenterAddress)
	settings.APRS.Server = clean(settings.APRS.Server)
	settings.APRS.LoginCallsign = strings.ToUpper(clean(settings.APRS.LoginCallsign))
	settings.APRS.ExtraFilter = clean(settings.APRS.ExtraFilter)
	settings.APRS.Mode = strings.ToLower(clean(settings.APRS.Mode))
	settings.APRS.Local.Decoder = strings.ToLower(clean(settings.APRS.Local.Decoder))
	settings.APRS.Local.KISSAddress = clean(settings.APRS.Local.KISSAddress)
	settings.APRS.Local.AudioDevice = clean(settings.APRS.Local.AudioDevice)
	settings.APRS.Local.AudioOutputDevice = clean(settings.APRS.Local.AudioOutputDevice)
	settings.Weather.AlertsURL = clean(settings.Weather.AlertsURL)
	settings.Weather.RadarURL = clean(settings.Weather.RadarURL)
	settings.Water.SiteIDs = clean(settings.Water.SiteIDs)
	settings.Water.SourceURL = clean(settings.Water.SourceURL)
	settings.Water.NOAAURL = clean(settings.Water.NOAAURL)
	settings.Integrations.StormReportsURL = clean(settings.Integrations.StormReportsURL)
	settings.Integrations.InfrastructureURL = clean(settings.Integrations.InfrastructureURL)
	settings.Integrations.AmateurRepeatersURL = clean(settings.Integrations.AmateurRepeatersURL)
	settings.Integrations.GMRSRepeatersURL = clean(settings.Integrations.GMRSRepeatersURL)
	settings.Integrations.MeshCoreURL = clean(settings.Integrations.MeshCoreURL)
	settings.Integrations.AREDNNodeURLs = clean(settings.Integrations.AREDNNodeURLs)
	settings.Integrations.SensorURLs = clean(settings.Integrations.SensorURLs)
	settings.APRS.Passcode = ""
	settings.APRS.PasscodeConfigured = s.APRSPasscode() != ""
	if settings.Organization == "" {
		return Settings{}, validationError{"organization name is required"}
	}
	if len(settings.CenterAddress) > 300 {
		return Settings{}, validationError{"map center address must be 300 characters or fewer"}
	}
	if !validCoordinates(&settings.CenterLat, &settings.CenterLon) {
		return Settings{}, validationError{"map center coordinates are invalid"}
	}
	if settings.DefaultZoom < 2 || settings.DefaultZoom > 18 {
		return Settings{}, validationError{"default map zoom must be between 2 and 18"}
	}
	if settings.IncidentStaleMinutes == 0 {
		settings.IncidentStaleMinutes = 10
	}
	if settings.IncidentStaleMinutes < 1 || settings.IncidentStaleMinutes > 1440 {
		return Settings{}, validationError{"incident update alert must be between 1 and 1,440 minutes"}
	}
	if settings.Weather.RefreshMinutes == 0 {
		settings.Weather.RefreshMinutes = 5
	}
	if settings.Weather.RefreshMinutes < 2 || settings.Weather.RefreshMinutes > 60 {
		return Settings{}, validationError{"weather refresh interval must be between 2 and 60 minutes"}
	}
	if settings.Weather.RadarOpacity == 0 {
		settings.Weather.RadarOpacity = 55
	}
	if settings.Weather.RadarOpacity < 10 || settings.Weather.RadarOpacity > 90 {
		return Settings{}, validationError{"weather radar opacity must be between 10 and 90 percent"}
	}
	if settings.Weather.RadarFrames == 0 {
		settings.Weather.RadarFrames = 4
	}
	if settings.Weather.RadarFrames < 2 || settings.Weather.RadarFrames > 6 {
		return Settings{}, validationError{"weather radar animation must use between 2 and 6 frames"}
	}
	if err := validateWeatherProviderURL(settings.Weather.AlertsURL, "weather alert"); err != nil {
		return Settings{}, err
	}
	if err := validateWeatherProviderURL(settings.Weather.RadarURL, "weather radar"); err != nil {
		return Settings{}, err
	}
	if settings.Water.RefreshMinutes == 0 {
		settings.Water.RefreshMinutes = 15
	}
	if settings.Water.RefreshMinutes < 5 || settings.Water.RefreshMinutes > 120 {
		return Settings{}, validationError{"water refresh interval must be between 5 and 120 minutes"}
	}
	if err := validateWeatherProviderURL(settings.Water.SourceURL, "water gauge"); err != nil {
		return Settings{}, err
	}
	if err := validateWeatherProviderURL(settings.Water.NOAAURL, "NOAA water"); err != nil {
		return Settings{}, err
	}
	if _, err := normalizeWaterSiteIDs(settings.Water.SiteIDs); err != nil {
		return Settings{}, err
	}
	if settings.Integrations.RefreshMinutes == 0 {
		settings.Integrations.RefreshMinutes = 5
	}
	if settings.Integrations.RefreshMinutes < 1 || settings.Integrations.RefreshMinutes > 120 {
		return Settings{}, validationError{"integration refresh interval must be between 1 and 120 minutes"}
	}
	for label, value := range map[string]string{"storm report": settings.Integrations.StormReportsURL, "infrastructure": settings.Integrations.InfrastructureURL, "amateur repeater": settings.Integrations.AmateurRepeatersURL, "GMRS repeater": settings.Integrations.GMRSRepeatersURL, "MeshCore": settings.Integrations.MeshCoreURL} {
		if err := validateWeatherProviderURL(value, label); err != nil {
			return Settings{}, err
		}
	}
	if _, err := normalizeEndpointList(settings.Integrations.AREDNNodeURLs, 10, "AREDN node"); err != nil {
		return Settings{}, err
	}
	if _, err := normalizeEndpointList(settings.Integrations.SensorURLs, 10, "sensor"); err != nil {
		return Settings{}, err
	}
	if settings.APRS.Server == "" {
		settings.APRS.Server = "rotate.aprs2.net:14580"
	}
	if _, _, err := net.SplitHostPort(settings.APRS.Server); err != nil {
		return Settings{}, validationError{"APRS-IS server must be a host and port, such as rotate.aprs2.net:14580"}
	}
	if settings.APRS.StaleMinutes < 1 || settings.APRS.StaleMinutes > 1440 {
		return Settings{}, validationError{"APRS stale threshold must be between 1 and 1,440 minutes"}
	}
	if settings.APRS.TrailHours < 1 || settings.APRS.TrailHours > 168 {
		return Settings{}, validationError{"APRS trail history must be between 1 and 168 hours"}
	}
	if settings.APRS.AreaRadiusMiles < 1 || settings.APRS.AreaRadiusMiles > 250 {
		return Settings{}, validationError{"APRS area radius must be between 1 and 250 miles"}
	}
	if settings.APRS.Mode == "" {
		settings.APRS.Mode = "internet"
	}
	if !oneOf(settings.APRS.Mode, "internet", "local", "hybrid") {
		return Settings{}, validationError{"APRS connection mode is invalid"}
	}
	if settings.APRS.Local.Decoder == "" {
		settings.APRS.Local.Decoder = "bundled"
	}
	if !oneOf(settings.APRS.Local.Decoder, "bundled", "kiss_tcp") {
		return Settings{}, validationError{"local APRS decoder selection is invalid"}
	}
	if settings.APRS.Local.KISSAddress == "" {
		settings.APRS.Local.KISSAddress = "127.0.0.1:8001"
	}
	if _, _, err := net.SplitHostPort(settings.APRS.Local.KISSAddress); err != nil {
		return Settings{}, validationError{"local KISS address must be a host and port, such as 127.0.0.1:8001"}
	}
	if len(settings.APRS.Local.AudioDevice) > 160 || strings.ContainsAny(settings.APRS.Local.AudioDevice, "\r\n") {
		return Settings{}, validationError{"local audio device name is invalid"}
	}
	if len(settings.APRS.Local.AudioOutputDevice) > 160 || strings.ContainsAny(settings.APRS.Local.AudioOutputDevice, "\r\n") {
		return Settings{}, validationError{"local audio output device name is invalid"}
	}
	usesBundledDecoder := settings.APRS.Enabled &&
		(settings.APRS.Mode == "local" || settings.APRS.Mode == "hybrid") &&
		settings.APRS.Local.Decoder == "bundled"
	if usesBundledDecoder && settings.APRS.Local.AudioDevice == "" {
		return Settings{}, validationError{"detect and select a radio audio input before enabling the built-in decoder"}
	}
	if usesBundledDecoder && settings.APRS.Local.AudioOutputDevice == "" {
		return Settings{}, validationError{"detect and select an audio output before enabling the built-in decoder"}
	}
	usesInternet := settings.APRS.Mode == "internet" || settings.APRS.Mode == "hybrid" || settings.APRS.Local.IGateEnabled
	if settings.APRS.Enabled && usesInternet && !validAPRSISLogin(settings.APRS.LoginCallsign) {
		return Settings{}, validationError{"a valid APRS-IS login callsign is required when APRS is enabled"}
	}
	if strings.ContainsAny(settings.APRS.ExtraFilter, "\r\n") || len(settings.APRS.ExtraFilter) > 300 {
		return Settings{}, validationError{"APRS extra filter is invalid"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	activity := newActivity("settings.updated", "System settings updated", "settings", "system")
	if _, err := s.commitLocked("settings.updated", changeSet{Settings: &settings, Activity: &activity}); err != nil {
		return Settings{}, err
	}
	s.trimTracks(time.Now().UTC())
	return settings, nil
}

func validateWeatherProviderURL(value, label string) error {
	if value == "" {
		return nil
	}
	if len(value) > 1000 {
		return validationError{label + " source URL must be 1,000 characters or fewer"}
	}
	parsed, err := url.Parse(value)
	if err != nil || !oneOf(parsed.Scheme, "http", "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return validationError{label + " source must be an HTTP or HTTPS URL without credentials or a fragment"}
	}
	return nil
}

func (s *Store) RecordAPRSPosition(position APRSPosition, source string) (Responder, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := slices.IndexFunc(s.state.Responders, func(item Responder) bool {
		return item.APRSEnabled && strings.EqualFold(item.Callsign, position.Callsign)
	})
	if index < 0 {
		return Responder{}, false, nil
	}

	responder := s.state.Responders[index]
	now := position.ReceivedAt.UTC()
	if source != "local_rf" {
		source = "aprs_is"
	}
	responder.Latitude = pointer(position.Latitude)
	responder.Longitude = pointer(position.Longitude)
	responder.PositionSource = source
	responder.PositionUpdatedAt = &now
	responder.SpeedKnots = position.SpeedKnots
	responder.CourseDegrees = position.CourseDegrees
	responder.AltitudeFeet = position.AltitudeFeet
	responder.APRSSymbol = position.Symbol
	responder.APRSComment = abbreviate(strings.TrimSpace(position.Comment), 500)

	track := TrackPoint{
		ID:            newID("trk"),
		ResponderID:   responder.ID,
		Callsign:      responder.Callsign,
		Latitude:      position.Latitude,
		Longitude:     position.Longitude,
		SpeedKnots:    position.SpeedKnots,
		CourseDegrees: position.CourseDegrees,
		AltitudeFeet:  position.AltitudeFeet,
		Symbol:        position.Symbol,
		Comment:       responder.APRSComment,
		Source:        source,
		ReceivedAt:    now,
	}
	if _, err := s.commitLocked("aprs.position", changeSet{Responder: &responder, Track: &track}); err != nil {
		return Responder{}, false, err
	}
	s.trimTracks(now)
	return responder, true, nil
}

func (s *Store) SeedDemoData() error {
	if len(s.Snapshot().Incidents)+len(s.Snapshot().Responders)+len(s.Snapshot().Facilities) > 0 {
		return validationError{"demo data can only be added to an empty system"}
	}
	lat1, lon1 := 35.9658, -86.6760
	lat2, lon2 := 35.9704, -86.6688
	unit1, err := s.CreateResponder(ResponderInput{Name: "Medic 1", Callsign: "M1", Type: "Medical", Status: "available", Capabilities: []string{"ALS", "Transport"}, Latitude: &lat1, Longitude: &lon1})
	if err != nil {
		return err
	}
	if _, err = s.CreateResponder(ResponderInput{Name: "Command 2", Callsign: "CMD2", Type: "Command", Status: "available", Capabilities: []string{"Incident command"}}); err != nil {
		return err
	}
	if _, err = s.CreateFacility(FacilityInput{Name: "Community Shelter", Type: "Shelter", Status: "open", Address: "100 Community Way", Capacity: 120, Occupied: 34, Latitude: &lat2, Longitude: &lon2}); err != nil {
		return err
	}
	incident, err := s.CreateIncident(IncidentInput{
		Title:       "Welfare check requested",
		Type:        "Welfare Check",
		Severity:    "medium",
		Status:      "new",
		Address:     "450 Main Street",
		City:        "Sample City",
		Region:      "TN",
		Latitude:    &lat2,
		Longitude:   &lon2,
		Description: "Demonstration incident. Delete the application data folder to return to a blank system.",
	})
	if err != nil {
		return err
	}
	_, err = s.AssignResponder(incident.ID, unit1.ID)
	return err
}

func (s *Store) commitLocked(kind string, change changeSet) (Event, error) {
	payload, err := json.Marshal(change)
	if err != nil {
		return Event{}, err
	}
	event := Event{
		ID:        newID("evt"),
		Kind:      kind,
		CreatedAt: time.Now().UTC(),
		Change:    payload,
	}
	line, err := json.Marshal(event)
	if err != nil {
		return Event{}, err
	}
	line = append(line, '\n')

	file, err := os.OpenFile(s.eventPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Event{}, fmt.Errorf("open event log for append: %w", err)
	}
	if _, err = file.Write(line); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return Event{}, fmt.Errorf("append event log: %w", err)
	}
	if closeErr != nil {
		return Event{}, fmt.Errorf("close event log: %w", closeErr)
	}
	if err := s.applyEvent(event); err != nil {
		return Event{}, err
	}
	for subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
	return event, nil
}

func (s *Store) applyEvent(event Event) error {
	var change changeSet
	if err := json.Unmarshal(event.Change, &change); err != nil {
		return err
	}
	if change.Incident != nil {
		upsertIncident(&s.state.Incidents, *change.Incident)
	}
	if change.Responder != nil {
		upsertResponder(&s.state.Responders, *change.Responder)
	}
	for _, responder := range change.Responders {
		upsertResponder(&s.state.Responders, responder)
	}
	if change.Facility != nil {
		upsertFacility(&s.state.Facilities, *change.Facility)
	}
	if change.Location != nil {
		upsertLocation(&s.state.Locations, *change.Location)
	}
	if change.LocationDeleteID != "" {
		s.state.Locations = slices.DeleteFunc(s.state.Locations, func(item Location) bool { return item.ID == change.LocationDeleteID })
	}
	if change.Overlay != nil {
		upsertOverlay(&s.state.Overlays, *change.Overlay)
	}
	if change.OverlayDeleteID != "" {
		s.state.Overlays = slices.DeleteFunc(s.state.Overlays, func(item MapOverlay) bool { return item.ID == change.OverlayDeleteID })
	}
	if change.Asset != nil {
		upsertAsset(&s.state.Assets, *change.Asset)
	}
	if change.AssetDeleteID != "" {
		s.state.Assets = slices.DeleteFunc(s.state.Assets, func(item Asset) bool { return item.ID == change.AssetDeleteID })
	}
	if change.ScheduleItem != nil {
		upsertScheduleItem(&s.state.Schedule, *change.ScheduleItem)
	}
	if change.ScheduleDeleteID != "" {
		s.state.Schedule = slices.DeleteFunc(s.state.Schedule, func(item ScheduleItem) bool { return item.ID == change.ScheduleDeleteID })
	}
	if change.Qualification != nil {
		upsertQualification(&s.state.Qualifications, *change.Qualification)
	}
	if change.QualificationDeleteID != "" {
		s.state.Qualifications = slices.DeleteFunc(s.state.Qualifications, func(item Qualification) bool { return item.ID == change.QualificationDeleteID })
	}
	if change.Message != nil {
		s.state.Messages = append(s.state.Messages, *change.Message)
		if len(s.state.Messages) > 5000 {
			s.state.Messages = slices.Clone(s.state.Messages[len(s.state.Messages)-5000:])
		}
	}
	if change.Settings != nil {
		s.state.Settings = *change.Settings
	}
	if change.Track != nil {
		s.state.Tracks = append(s.state.Tracks, *change.Track)
	}
	if change.TrackDeleteResponderID != "" {
		s.state.Tracks = slices.DeleteFunc(s.state.Tracks, func(item TrackPoint) bool {
			return item.ResponderID == change.TrackDeleteResponderID
		})
	}
	if change.Activity != nil {
		s.state.Activity = append([]Activity{*change.Activity}, s.state.Activity...)
		if len(s.state.Activity) > 1000 {
			s.state.Activity = s.state.Activity[:1000]
		}
	}
	return nil
}

func (s *Store) applySettingDefaults() {
	s.state.SchemaVersion = 3
	if s.state.Settings.IncidentStaleMinutes == 0 {
		s.state.Settings.IncidentStaleMinutes = 10
	}
	if s.state.Settings.Weather.RefreshMinutes == 0 {
		s.state.Settings.Weather.RefreshMinutes = 5
	}
	if s.state.Settings.Weather.RadarOpacity == 0 {
		s.state.Settings.Weather.RadarOpacity = 55
	}
	if s.state.Settings.Weather.RadarFrames == 0 {
		s.state.Settings.Weather.RadarFrames = 4
	}
	if s.state.Settings.Water.RefreshMinutes == 0 {
		s.state.Settings.Water.RefreshMinutes = 15
	}
	if s.state.Settings.Integrations.RefreshMinutes == 0 {
		s.state.Settings.Integrations.RefreshMinutes = 5
	}
	if s.state.Settings.APRS.Server == "" {
		s.state.Settings.APRS.Server = "rotate.aprs2.net:14580"
	}
	if s.state.Settings.APRS.Mode == "" {
		s.state.Settings.APRS.Mode = "internet"
	}
	if s.state.Settings.APRS.StaleMinutes == 0 {
		s.state.Settings.APRS.StaleMinutes = 15
	}
	if s.state.Settings.APRS.TrailHours == 0 {
		s.state.Settings.APRS.TrailHours = 12
	}
	if s.state.Settings.APRS.AreaRadiusMiles == 0 {
		s.state.Settings.APRS.AreaRadiusMiles = 25
	}
	if s.state.Settings.APRS.Local.Decoder == "" {
		s.state.Settings.APRS.Local.Decoder = "bundled"
		s.state.Settings.APRS.Local.ShowAll = true
	}
	if s.state.Settings.APRS.Local.KISSAddress == "" {
		s.state.Settings.APRS.Local.KISSAddress = "127.0.0.1:8001"
	}
	s.state.Settings.APRS.Passcode = ""
	s.state.Settings.APRS.PasscodeConfigured = s.APRSPasscode() != ""
	if s.state.Tracks == nil {
		s.state.Tracks = []TrackPoint{}
	}
	if s.state.Locations == nil {
		s.state.Locations = []Location{}
	}
	if s.state.Overlays == nil {
		s.state.Overlays = []MapOverlay{}
	}
	if s.state.Assets == nil {
		s.state.Assets = []Asset{}
	}
	if s.state.Schedule == nil {
		s.state.Schedule = []ScheduleItem{}
	}
	if s.state.Qualifications == nil {
		s.state.Qualifications = []Qualification{}
	}
	if s.state.Messages == nil {
		s.state.Messages = []OperationalMessage{}
	}
	s.state.APRSStations = []APRSStation{}
}

func validAPRSPasscode(passcode string) bool {
	if len(passcode) < 1 || len(passcode) > 5 {
		return false
	}
	for _, character := range passcode {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func staleUpdate(expected *time.Time, current time.Time) bool {
	return expected != nil && !expected.UTC().Equal(current.UTC())
}

func (s *Store) trimTracks(now time.Time) {
	cutoff := now.Add(-time.Duration(s.state.Settings.APRS.TrailHours) * time.Hour)
	s.state.Tracks = slices.DeleteFunc(s.state.Tracks, func(point TrackPoint) bool {
		return point.ReceivedAt.Before(cutoff)
	})
	if len(s.state.Tracks) > 10000 {
		s.state.Tracks = slices.Clone(s.state.Tracks[len(s.state.Tracks)-10000:])
	}
}

func (s *Store) nextIncidentNumber() int64 {
	var max int64 = 1000
	for _, incident := range s.state.Incidents {
		if incident.Number > max {
			max = incident.Number
		}
	}
	return max + 1
}

func validateIncidentInput(input IncidentInput, requireStatus bool) error {
	if clean(input.Title) == "" {
		return validationError{"incident title is required"}
	}
	if len(input.Title) > 160 {
		return validationError{"incident title must be 160 characters or fewer"}
	}
	if clean(input.Type) == "" {
		return validationError{"incident type is required"}
	}
	if clean(input.Address) == "" {
		return validationError{"incident location is required"}
	}
	if !oneOf(input.Severity, "critical", "high", "medium", "low") {
		return validationError{"incident severity is invalid"}
	}
	if requireStatus && !oneOf(input.Status, "new", "assigned", "enroute", "onscene", "closed", "cancelled") {
		return validationError{"incident status is invalid"}
	}
	if !validOptionalCoordinates(input.Latitude, input.Longitude) {
		return validationError{"incident coordinates are invalid"}
	}
	if len(input.Description) > 20000 {
		return validationError{"incident description must be 20,000 characters or fewer"}
	}
	if input.Command != nil {
		if len(*input.Command) > 12 {
			return validationError{"incident command structure may contain no more than 12 roles"}
		}
		seenRoles := map[string]bool{}
		seenResponders := map[string]bool{}
		for _, assignment := range *input.Command {
			if !oneOf(assignment.Role, "incident_commander", "public_information", "safety", "liaison", "operations", "planning", "logistics", "finance") {
				return validationError{"incident command role is invalid"}
			}
			if clean(assignment.ResponderID) == "" {
				return validationError{"incident command responder is required"}
			}
			if seenRoles[assignment.Role] || seenResponders[assignment.ResponderID] {
				return validationError{"incident command roles and responders must be unique"}
			}
			seenRoles[assignment.Role], seenResponders[assignment.ResponderID] = true, true
		}
	}
	return nil
}

func buildCommandRoles(existing []CommandRole, input *[]CommandRoleInput, responders []Responder, now time.Time) ([]CommandRole, error) {
	if input == nil {
		return []CommandRole{}, nil
	}
	result := make([]CommandRole, 0, len(*input))
	for _, proposed := range *input {
		if findResponder(responders, proposed.ResponderID) < 0 {
			return nil, validationError{"incident command responder does not exist"}
		}
		assignedAt := now
		for _, current := range existing {
			if current.Role == proposed.Role && current.ResponderID == proposed.ResponderID {
				assignedAt = current.AssignedAt
				break
			}
		}
		result = append(result, CommandRole{Role: proposed.Role, ResponderID: proposed.ResponderID, AssignedAt: assignedAt})
	}
	return result, nil
}

func validateResponderInput(input ResponderInput) error {
	if clean(input.Name) == "" && clean(input.Callsign) == "" {
		return validationError{"responder name or callsign is required"}
	}
	if !oneOf(defaultString(input.Status, "available"), "available", "assigned", "enroute", "onscene", "out_of_service") {
		return validationError{"responder status is invalid"}
	}
	if !validOptionalCoordinates(input.Latitude, input.Longitude) {
		return validationError{"responder coordinates are invalid"}
	}
	if input.APRSEnabled && !validTrackedCallsign(strings.ToUpper(clean(input.Callsign))) {
		return validationError{"a valid callsign with optional SSID is required for APRS tracking"}
	}
	if !validMapAppearance(input.MapLabel, input.MarkerColor) {
		return validationError{"map label must be 2–3 letters or numbers and color must be a hexadecimal color"}
	}
	return nil
}

func validateFacilityInput(input FacilityInput) error {
	if clean(input.Name) == "" {
		return validationError{"facility name is required"}
	}
	if !oneOf(defaultString(input.Status, "open"), "open", "limited", "closed") {
		return validationError{"facility status is invalid"}
	}
	if input.Capacity < 0 || input.Occupied < 0 {
		return validationError{"capacity values cannot be negative"}
	}
	if input.Capacity > 0 && input.Occupied > input.Capacity {
		return validationError{"occupied capacity cannot exceed total capacity"}
	}
	if !validOptionalCoordinates(input.Latitude, input.Longitude) {
		return validationError{"facility coordinates are invalid"}
	}
	if !validMapAppearance(input.MapLabel, input.MarkerColor) {
		return validationError{"map label must be 2–3 letters or numbers and color must be a hexadecimal color"}
	}
	return nil
}

func validMapAppearance(mapLabel, markerColor string) bool {
	label := normalizeMapLabel(mapLabel)
	if label != "" && (len(label) < 2 || len(label) > 3) {
		return false
	}
	for _, character := range label {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	color := strings.TrimSpace(markerColor)
	return color == "" || validOverlayColor(color)
}

func normalizeMapLabel(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeMarkerColor(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateLocationInput(input LocationInput) error {
	if clean(input.Name) == "" {
		return validationError{"location name is required"}
	}
	if len(input.Name) > 120 {
		return validationError{"location name must be 120 characters or fewer"}
	}
	if clean(input.Address) == "" {
		return validationError{"location address or description is required"}
	}
	if !validOptionalCoordinates(input.Latitude, input.Longitude) {
		return validationError{"location coordinates are invalid"}
	}
	if len(input.Notes) > 4000 {
		return validationError{"location notes must be 4,000 characters or fewer"}
	}
	return nil
}

func validateOverlayInput(input MapOverlayInput) error {
	if clean(input.Name) == "" {
		return validationError{"overlay name is required"}
	}
	if len(input.Name) > 160 || len(input.FileName) > 255 {
		return validationError{"overlay name or file name is too long"}
	}
	if !validOverlayColor(input.Color) {
		return validationError{"overlay color must be a six-digit hex color"}
	}
	if len(input.Features) == 0 {
		return validationError{"KML file does not contain supported map geometry"}
	}
	if len(input.Features) > 2000 {
		return validationError{"overlay contains more than 2,000 features"}
	}
	points := 0
	for _, feature := range input.Features {
		if !oneOf(feature.GeometryType, "point", "line", "polygon") {
			return validationError{"overlay contains an unsupported geometry type"}
		}
		if len(feature.Name) > 300 || len(feature.Paths) == 0 {
			return validationError{"overlay contains an invalid feature"}
		}
		for _, path := range feature.Paths {
			if len(path) == 0 {
				return validationError{"overlay contains an empty path"}
			}
			points += len(path)
			for _, coordinate := range path {
				if !validCoordinates(&coordinate.Latitude, &coordinate.Longitude) {
					return validationError{"overlay contains invalid coordinates"}
				}
			}
		}
	}
	if points > 50000 {
		return validationError{"overlay contains more than 50,000 coordinates"}
	}
	return nil
}

func validateOverlayUpdateInput(input MapOverlayUpdateInput) error {
	if clean(input.Name) == "" || len(input.Name) > 160 {
		return validationError{"overlay name is required and must be 160 characters or fewer"}
	}
	if !validOverlayColor(input.Color) {
		return validationError{"overlay color must be a six-digit hex color"}
	}
	return nil
}

func validOverlayColor(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, character := range value[1:] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func normalizeOverlayColor(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "#A78BFA"
	}
	return value
}

func validOptionalCoordinates(lat, lon *float64) bool {
	if lat == nil && lon == nil {
		return true
	}
	if lat == nil || lon == nil {
		return false
	}
	return validCoordinates(lat, lon)
}

func validateAssetInput(input AssetInput) error {
	if !oneOf(input.Category, "vehicle", "equipment") {
		return validationError{"asset category must be vehicle or equipment"}
	}
	if clean(input.Name) == "" || len(input.Name) > 120 {
		return validationError{"asset name is required and must be 120 characters or fewer"}
	}
	if len(input.Identifier) > 80 || len(input.Location) > 160 || len(input.Custodian) > 120 || len(input.Notes) > 4000 {
		return validationError{"asset details exceed the allowed length"}
	}
	if !oneOf(defaultString(input.Status, "ready"), "ready", "assigned", "maintenance", "out_of_service", "retired") {
		return validationError{"asset status is invalid"}
	}
	if input.Quantity < 1 || input.Quantity > 100000 {
		return validationError{"asset quantity must be between 1 and 100,000"}
	}
	return nil
}

func validateScheduleItemInput(input ScheduleItemInput) error {
	if clean(input.Title) == "" || len(input.Title) > 160 {
		return validationError{"schedule title is required and must be 160 characters or fewer"}
	}
	if !oneOf(input.Type, "shift", "exercise", "training", "maintenance", "meeting", "other") {
		return validationError{"schedule type is invalid"}
	}
	if !oneOf(defaultString(input.Status, "planned"), "planned", "confirmed", "in_progress", "completed", "cancelled") {
		return validationError{"schedule status is invalid"}
	}
	if input.StartAt.IsZero() || input.EndAt.IsZero() || !input.EndAt.After(input.StartAt) || input.EndAt.Sub(input.StartAt) > 31*24*time.Hour {
		return validationError{"schedule end must follow its start and be within 31 days"}
	}
	if len(input.Location) > 160 || len(input.Coordinator) > 120 || len(input.Notes) > 4000 || len(input.AssignedResponderIDs) > 100 {
		return validationError{"schedule details exceed the allowed length"}
	}
	return nil
}

func scheduleRespondersExist(responders []Responder, ids []string) bool {
	for _, id := range cleanList(ids) {
		if findResponder(responders, id) < 0 {
			return false
		}
	}
	return true
}

func validateQualificationInput(input QualificationInput) error {
	if clean(input.ResponderID) == "" {
		return validationError{"responder is required"}
	}
	if !oneOf(input.Category, "certification", "course", "license", "exercise", "other") {
		return validationError{"qualification category is invalid"}
	}
	if clean(input.Name) == "" || len(input.Name) > 160 {
		return validationError{"qualification name is required and must be 160 characters or fewer"}
	}
	if !oneOf(defaultString(input.Status, "current"), "current", "pending", "expired", "revoked") {
		return validationError{"qualification status is invalid"}
	}
	if len(input.Provider) > 160 || len(input.CredentialID) > 100 || len(input.Notes) > 4000 {
		return validationError{"qualification details exceed the allowed length"}
	}
	if input.CompletedAt != nil && input.ExpiresAt != nil && input.ExpiresAt.Before(*input.CompletedAt) {
		return validationError{"expiration date cannot precede completion"}
	}
	return nil
}

func validateOperationalMessageInput(input OperationalMessageInput) error {
	if !oneOf(defaultString(input.Channel, "general"), "general", "command", "operations", "logistics", "medical") {
		return validationError{"message channel is invalid"}
	}
	if len(input.Author) > 100 {
		return validationError{"message author must be 100 characters or fewer"}
	}
	body := strings.TrimSpace(input.Body)
	if body == "" || len(body) > 5000 {
		return validationError{"message body is required and must be 5,000 characters or fewer"}
	}
	return nil
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := value.UTC()
	return &converted
}

func validCoordinates(lat, lon *float64) bool {
	return lat != nil && lon != nil && *lat >= -90 && *lat <= 90 && *lon >= -180 && *lon <= 180
}

func validTrackedCallsign(value string) bool {
	if value == "" || len(value) > 9 || strings.Count(value, "-") > 1 {
		return false
	}
	parts := strings.Split(value, "-")
	if len(parts[0]) < 1 || len(parts[0]) > 6 || !asciiAlphaNumeric(parts[0]) {
		return false
	}
	return len(parts) == 1 || (len(parts[1]) >= 1 && len(parts[1]) <= 2 && asciiAlphaNumeric(parts[1]))
}

func validAPRSISLogin(value string) bool {
	if len(value) < 3 || len(value) > 9 || strings.HasSuffix(value, "-0") || !validTrackedCallsign(value) {
		return false
	}
	return true
}

func asciiAlphaNumeric(value string) bool {
	for _, character := range value {
		if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')) {
			return false
		}
	}
	return value != ""
}

func newActivity(kind, summary, entityType, entityID string) Activity {
	return Activity{
		ID:         newID("log"),
		Kind:       kind,
		Summary:    summary,
		EntityType: entityType,
		EntityID:   entityID,
		CreatedAt:  time.Now().UTC(),
	}
}

func newID(prefix string) string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(random)
}

func clean(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func cleanList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = clean(value)
		key := strings.ToLower(value)
		if value != "" && !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result
}

func oneOf(value string, allowed ...string) bool {
	return slices.Contains(allowed, value)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func pointer[T any](value T) *T {
	return &value
}

func label(value string) string {
	return strings.Title(strings.ReplaceAll(value, "_", " "))
}

func abbreviate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}

func responderDisplayName(responder Responder) string {
	if responder.Callsign != "" {
		return responder.Callsign
	}
	return responder.Name
}

func activeIncidentStatus(status string) bool {
	return oneOf(status, "new", "assigned", "enroute", "onscene")
}

func findIncident(items []Incident, id string) int {
	return slices.IndexFunc(items, func(item Incident) bool { return item.ID == id })
}

func findResponder(items []Responder, id string) int {
	return slices.IndexFunc(items, func(item Responder) bool { return item.ID == id })
}

func findFacility(items []Facility, id string) int {
	return slices.IndexFunc(items, func(item Facility) bool { return item.ID == id })
}

func findLocation(items []Location, id string) int {
	return slices.IndexFunc(items, func(item Location) bool { return item.ID == id })
}

func findOverlay(items []MapOverlay, id string) int {
	return slices.IndexFunc(items, func(item MapOverlay) bool { return item.ID == id })
}

func findAsset(items []Asset, id string) int {
	return slices.IndexFunc(items, func(item Asset) bool { return item.ID == id })
}

func findScheduleItem(items []ScheduleItem, id string) int {
	return slices.IndexFunc(items, func(item ScheduleItem) bool { return item.ID == id })
}

func findQualification(items []Qualification, id string) int {
	return slices.IndexFunc(items, func(item Qualification) bool { return item.ID == id })
}

func upsertIncident(items *[]Incident, value Incident) {
	if index := findIncident(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}

func upsertResponder(items *[]Responder, value Responder) {
	if index := findResponder(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}

func upsertFacility(items *[]Facility, value Facility) {
	if index := findFacility(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}

func upsertLocation(items *[]Location, value Location) {
	if index := findLocation(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}

func upsertOverlay(items *[]MapOverlay, value MapOverlay) {
	if index := findOverlay(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}

func upsertAsset(items *[]Asset, value Asset) {
	if index := findAsset(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}

func upsertScheduleItem(items *[]ScheduleItem, value ScheduleItem) {
	if index := findScheduleItem(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}

func upsertQualification(items *[]Qualification, value Qualification) {
	if index := findQualification(*items, value.ID); index >= 0 {
		(*items)[index] = value
	} else {
		*items = append(*items, value)
	}
}
