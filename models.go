package main

import "time"

const (
	productName = "Tickets Local"
	version     = "0.5.14"
)

type State struct {
	SchemaVersion  int                  `json:"schema_version"`
	Settings       Settings             `json:"settings"`
	Incidents      []Incident           `json:"incidents"`
	Responders     []Responder          `json:"responders"`
	Facilities     []Facility           `json:"facilities"`
	Locations      []Location           `json:"locations"`
	Overlays       []MapOverlay         `json:"overlays"`
	Assets         []Asset              `json:"assets"`
	Schedule       []ScheduleItem       `json:"schedule"`
	Qualifications []Qualification      `json:"qualifications"`
	Messages       []OperationalMessage `json:"messages"`
	Tracks         []TrackPoint         `json:"tracks"`
	APRSStations   []APRSStation        `json:"aprs_stations"`
	Activity       []Activity           `json:"activity"`
	APRSStatus     APRSStatus           `json:"aprs_status"`
}

type ConnectionDiagnostics struct {
	CheckedAt time.Time         `json:"checked_at"`
	Overall   string            `json:"overall"`
	Items     []ConnectionCheck `json:"items"`
}

type ConnectionCheck struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	State      string     `json:"state"`
	Detail     string     `json:"detail"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

type Settings struct {
	Organization         string              `json:"organization"`
	CenterAddress        string              `json:"center_address"`
	CenterLat            float64             `json:"center_lat"`
	CenterLon            float64             `json:"center_lon"`
	DefaultZoom          int                 `json:"default_zoom"`
	IncidentStaleMinutes int                 `json:"incident_stale_minutes"`
	APRS                 APRSSettings        `json:"aprs"`
	Weather              WeatherSettings     `json:"weather"`
	Water                WaterSettings       `json:"water"`
	Integrations         IntegrationSettings `json:"integrations"`
}

type IntegrationSettings struct {
	Enabled             bool   `json:"enabled"`
	RefreshMinutes      int    `json:"refresh_minutes"`
	StormReportsURL     string `json:"storm_reports_url"`
	InfrastructureURL   string `json:"infrastructure_url"`
	AmateurRepeatersURL string `json:"amateur_repeaters_url"`
	AmateurOSMEnabled   bool   `json:"amateur_osm_enabled"`
	GMRSRepeatersURL    string `json:"gmrs_repeaters_url"`
	MeshCoreURL         string `json:"meshcore_url"`
	AREDNNodeURLs       string `json:"aredn_node_urls"`
	SensorURLs          string `json:"sensor_urls"`
}

type IntegrationStatus struct {
	State            string               `json:"state"`
	Message          string               `json:"message"`
	UpdatedAt        *time.Time           `json:"updated_at,omitempty"`
	Stale            bool                 `json:"stale"`
	StormReports     []ExternalMapFeature `json:"storm_reports"`
	Infrastructure   []ExternalMapFeature `json:"infrastructure"`
	AmateurRepeaters []ExternalMapFeature `json:"amateur_repeaters"`
	GMRSRepeaters    []ExternalMapFeature `json:"gmrs_repeaters"`
	MeshCoreNodes    []ExternalMapFeature `json:"meshcore_nodes"`
	AREDNNodes       []AREDNNodeStatus    `json:"aredn_nodes"`
	Sensors          []SensorObservation  `json:"sensors"`
}
type ExternalMapFeature struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Source     string     `json:"source"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Latitude   float64    `json:"latitude"`
	Longitude  float64    `json:"longitude"`
	Details    string     `json:"details"`
}
type AREDNNodeStatus struct {
	URL       string    `json:"url"`
	Name      string    `json:"name"`
	Reachable bool      `json:"reachable"`
	RouteCost *float64  `json:"route_cost,omitempty"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
	Message   string    `json:"message"`
}
type SensorObservation struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Source          string    `json:"source"`
	ObservedAt      time.Time `json:"observed_at"`
	Latitude        *float64  `json:"latitude,omitempty"`
	Longitude       *float64  `json:"longitude,omitempty"`
	TemperatureF    *float64  `json:"temperature_f,omitempty"`
	HumidityPercent *float64  `json:"humidity_percent,omitempty"`
	PressureInHg    *float64  `json:"pressure_in_hg,omitempty"`
	WindSpeedMPH    *float64  `json:"wind_speed_mph,omitempty"`
	WaterDepthFeet  *float64  `json:"water_depth_feet,omitempty"`
	BatteryPercent  *float64  `json:"battery_percent,omitempty"`
	Status          string    `json:"status"`
}

type WaterSettings struct {
	Enabled        bool   `json:"enabled"`
	SiteIDs        string `json:"site_ids"`
	RefreshMinutes int    `json:"refresh_minutes"`
	SourceURL      string `json:"source_url"`
	NOAAEnabled    bool   `json:"noaa_enabled"`
	NOAAURL        string `json:"noaa_url"`
}

type WaterStatus struct {
	State     string       `json:"state"`
	Message   string       `json:"message"`
	UpdatedAt *time.Time   `json:"updated_at,omitempty"`
	Stale     bool         `json:"stale"`
	Source    string       `json:"source"`
	Gauges    []WaterGauge `json:"gauges"`
}

type WaterGauge struct {
	SiteID                string     `json:"site_id"`
	Name                  string     `json:"name"`
	Latitude              float64    `json:"latitude"`
	Longitude             float64    `json:"longitude"`
	ObservedAt            *time.Time `json:"observed_at,omitempty"`
	StageFeet             *float64   `json:"stage_feet,omitempty"`
	FlowCFS               *float64   `json:"flow_cfs,omitempty"`
	StageTrend            string     `json:"stage_trend"`
	FlowTrend             string     `json:"flow_trend"`
	Provisional           bool       `json:"provisional"`
	FloodCategory         string     `json:"flood_category"`
	ForecastFloodCategory string     `json:"forecast_flood_category"`
	FloodStageFeet        *float64   `json:"flood_stage_feet,omitempty"`
	ForecastStageFeet     *float64   `json:"forecast_stage_feet,omitempty"`
	ForecastAt            *time.Time `json:"forecast_at,omitempty"`
}

type WaterSiteCandidate struct {
	SiteID        string  `json:"site_id"`
	Name          string  `json:"name"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	DistanceMiles float64 `json:"distance_miles"`
}

type WeatherSettings struct {
	Enabled        bool   `json:"enabled"`
	RefreshMinutes int    `json:"refresh_minutes"`
	RadarEnabled   bool   `json:"radar_enabled"`
	RadarOpacity   int    `json:"radar_opacity"`
	RadarAnimation bool   `json:"radar_animation"`
	RadarFrames    int    `json:"radar_frames"`
	AlertsURL      string `json:"alerts_url"`
	RadarURL       string `json:"radar_url"`
}

type WeatherStatus struct {
	State     string          `json:"state"`
	Message   string          `json:"message"`
	UpdatedAt *time.Time      `json:"updated_at,omitempty"`
	Stale     bool            `json:"stale"`
	Source    string          `json:"source"`
	CenterLat float64         `json:"center_lat"`
	CenterLon float64         `json:"center_lon"`
	Alerts    []WeatherAlert  `json:"alerts"`
	Current   *CurrentWeather `json:"current,omitempty"`
}

type CurrentWeather struct {
	Station              string     `json:"station"`
	Description          string     `json:"description"`
	ObservedAt           *time.Time `json:"observed_at,omitempty"`
	TemperatureF         *float64   `json:"temperature_f,omitempty"`
	FeelsLikeF           *float64   `json:"feels_like_f,omitempty"`
	HumidityPercent      *float64   `json:"humidity_percent,omitempty"`
	WindSpeedMPH         *float64   `json:"wind_speed_mph,omitempty"`
	WindDirectionDegrees *float64   `json:"wind_direction_degrees,omitempty"`
	BarometerInHg        *float64   `json:"barometer_in_hg,omitempty"`
	VisibilityMiles      *float64   `json:"visibility_miles,omitempty"`
}

type WeatherAlert struct {
	ID          string            `json:"id"`
	Event       string            `json:"event"`
	Headline    string            `json:"headline"`
	Severity    string            `json:"severity"`
	Urgency     string            `json:"urgency"`
	Certainty   string            `json:"certainty"`
	Description string            `json:"description"`
	Instruction string            `json:"instruction"`
	Area        string            `json:"area"`
	Effective   *time.Time        `json:"effective,omitempty"`
	Expires     *time.Time        `json:"expires,omitempty"`
	Paths       [][]MapCoordinate `json:"paths"`
}

type APRSSettings struct {
	Enabled            bool              `json:"enabled"`
	Mode               string            `json:"mode"`
	Server             string            `json:"server"`
	LoginCallsign      string            `json:"login_callsign"`
	ExtraFilter        string            `json:"extra_filter"`
	WatchCallsigns     []string          `json:"watch_callsigns"`
	StaleMinutes       int               `json:"stale_minutes"`
	TrailHours         int               `json:"trail_hours"`
	AreaEnabled        bool              `json:"area_enabled"`
	AreaRadiusMiles    int               `json:"area_radius_miles"`
	Passcode           string            `json:"passcode,omitempty"`
	PasscodeConfigured bool              `json:"passcode_configured"`
	Local              APRSLocalSettings `json:"local"`
}

type APRSLocalSettings struct {
	Decoder           string `json:"decoder"`
	KISSAddress       string `json:"kiss_address"`
	AudioDevice       string `json:"audio_device"`
	AudioOutputDevice string `json:"audio_output_device"`
	ShowAll           bool   `json:"show_all"`
	IGateEnabled      bool   `json:"igate_enabled"`
}

type APRSStatus struct {
	Mode                   string          `json:"mode"`
	State                  string          `json:"state"`
	Message                string          `json:"message"`
	Server                 string          `json:"server"`
	Filter                 string          `json:"filter"`
	ConnectedAt            *time.Time      `json:"connected_at,omitempty"`
	LastPacketAt           *time.Time      `json:"last_packet_at,omitempty"`
	LastPositionAt         *time.Time      `json:"last_position_at,omitempty"`
	LastAreaPositionAt     *time.Time      `json:"last_area_position_at,omitempty"`
	PacketsReceived        uint64          `json:"packets_received"`
	PositionPacketsDecoded uint64          `json:"position_packets_decoded"`
	PositionsUpdated       uint64          `json:"positions_updated"`
	AreaPositionsReceived  uint64          `json:"area_positions_received"`
	AreaStations           int             `json:"area_stations"`
	Reconnects             uint64          `json:"reconnects"`
	LoginVerified          bool            `json:"login_verified"`
	LoginVerifiedAt        *time.Time      `json:"login_verified_at,omitempty"`
	Local                  APRSLocalStatus `json:"local"`
}

type APRSLocalStatus struct {
	State                  string     `json:"state"`
	Message                string     `json:"message"`
	Decoder                string     `json:"decoder"`
	KISSAddress            string     `json:"kiss_address"`
	ConnectedAt            *time.Time `json:"connected_at,omitempty"`
	LastPacketAt           *time.Time `json:"last_packet_at,omitempty"`
	LastPositionAt         *time.Time `json:"last_position_at,omitempty"`
	PacketsReceived        uint64     `json:"packets_received"`
	PositionPacketsDecoded uint64     `json:"position_packets_decoded"`
	PositionsDisplayed     uint64     `json:"positions_displayed"`
	PositionsUpdated       uint64     `json:"positions_updated"`
	Reconnects             uint64     `json:"reconnects"`
	DecoderRunning         bool       `json:"decoder_running"`
	DecoderAvailable       bool       `json:"decoder_available"`
	DecoderVersion         string     `json:"decoder_version,omitempty"`
	DecoderArchitecture    string     `json:"decoder_architecture,omitempty"`
	AudioLevel             *int       `json:"audio_level,omitempty"`
	RecentMessages         []string   `json:"recent_messages"`
	IGateEnabled           bool       `json:"igate_enabled"`
	IGateVerified          bool       `json:"igate_verified"`
}

type APRSAudioDevice struct {
	Name              string `json:"name"`
	HostAPI           string `json:"host_api,omitempty"`
	MaxInputChannels  int    `json:"max_input_channels"`
	MaxOutputChannels int    `json:"max_output_channels"`
	DefaultInput      bool   `json:"default_input"`
	DefaultOutput     bool   `json:"default_output"`
}

type APRSAudioDeviceDiscovery struct {
	DecoderAvailable    bool              `json:"decoder_available"`
	DecoderVersion      string            `json:"decoder_version,omitempty"`
	DecoderArchitecture string            `json:"decoder_architecture,omitempty"`
	Devices             []APRSAudioDevice `json:"devices"`
	Message             string            `json:"message"`
}

type APRSStation struct {
	Callsign        string                  `json:"callsign"`
	Latitude        float64                 `json:"latitude"`
	Longitude       float64                 `json:"longitude"`
	SpeedKnots      *float64                `json:"speed_knots,omitempty"`
	CourseDegrees   *float64                `json:"course_degrees,omitempty"`
	AltitudeFeet    *float64                `json:"altitude_feet,omitempty"`
	Symbol          string                  `json:"symbol,omitempty"`
	Comment         string                  `json:"comment,omitempty"`
	Source          string                  `json:"source,omitempty"`
	LastHeardAt     time.Time               `json:"last_heard_at"`
	PositionPackets uint64                  `json:"position_packets"`
	Weather         *APRSWeatherObservation `json:"weather,omitempty"`
}

type APRSWeatherObservation struct {
	ObservedAt              time.Time `json:"observed_at"`
	TemperatureF            *float64  `json:"temperature_f,omitempty"`
	WindDirection           *float64  `json:"wind_direction_degrees,omitempty"`
	WindSpeedMPH            *float64  `json:"wind_speed_mph,omitempty"`
	WindGustMPH             *float64  `json:"wind_gust_mph,omitempty"`
	RainLastHourInches      *float64  `json:"rain_last_hour_inches,omitempty"`
	Rain24HoursInches       *float64  `json:"rain_24_hours_inches,omitempty"`
	RainSinceMidnightInches *float64  `json:"rain_since_midnight_inches,omitempty"`
	HumidityPercent         *float64  `json:"humidity_percent,omitempty"`
	PressureMillibars       *float64  `json:"pressure_millibars,omitempty"`
}

type Incident struct {
	ID           string               `json:"id"`
	Number       int64                `json:"number"`
	Title        string               `json:"title"`
	Type         string               `json:"type"`
	Severity     string               `json:"severity"`
	Status       string               `json:"status"`
	Address      string               `json:"address"`
	City         string               `json:"city"`
	Region       string               `json:"region"`
	PostalCode   string               `json:"postal_code"`
	Latitude     *float64             `json:"latitude,omitempty"`
	Longitude    *float64             `json:"longitude,omitempty"`
	ContactName  string               `json:"contact_name"`
	ContactPhone string               `json:"contact_phone"`
	Description  string               `json:"description"`
	Assignments  []Assignment         `json:"assignments"`
	Actions      []Action             `json:"actions"`
	Attachments  []IncidentAttachment `json:"attachments"`
	Command      []CommandRole        `json:"command"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	ClosedAt     *time.Time           `json:"closed_at,omitempty"`
}

type IncidentAttachment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	MediaType   string    `json:"media_type"`
	Size        int64     `json:"size"`
	StorageName string    `json:"storage_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Assignment struct {
	ResponderID string    `json:"responder_id"`
	AssignedAt  time.Time `json:"assigned_at"`
}

type CommandRole struct {
	Role        string    `json:"role"`
	ResponderID string    `json:"responder_id"`
	AssignedAt  time.Time `json:"assigned_at"`
}

type CommandRoleInput struct {
	Role        string `json:"role"`
	ResponderID string `json:"responder_id"`
}

type Action struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type Responder struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Callsign          string     `json:"callsign"`
	Type              string     `json:"type"`
	Status            string     `json:"status"`
	Phone             string     `json:"phone"`
	Capabilities      []string   `json:"capabilities"`
	Latitude          *float64   `json:"latitude,omitempty"`
	Longitude         *float64   `json:"longitude,omitempty"`
	APRSEnabled       bool       `json:"aprs_enabled"`
	PositionSource    string     `json:"position_source,omitempty"`
	PositionUpdatedAt *time.Time `json:"position_updated_at,omitempty"`
	SpeedKnots        *float64   `json:"speed_knots,omitempty"`
	CourseDegrees     *float64   `json:"course_degrees,omitempty"`
	AltitudeFeet      *float64   `json:"altitude_feet,omitempty"`
	APRSSymbol        string     `json:"aprs_symbol,omitempty"`
	APRSComment       string     `json:"aprs_comment,omitempty"`
	MapLabel          string     `json:"map_label,omitempty"`
	MarkerColor       string     `json:"marker_color,omitempty"`
	Notes             string     `json:"notes"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type TrackPoint struct {
	ID            string    `json:"id"`
	ResponderID   string    `json:"responder_id"`
	Callsign      string    `json:"callsign"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	SpeedKnots    *float64  `json:"speed_knots,omitempty"`
	CourseDegrees *float64  `json:"course_degrees,omitempty"`
	AltitudeFeet  *float64  `json:"altitude_feet,omitempty"`
	Symbol        string    `json:"symbol,omitempty"`
	Comment       string    `json:"comment,omitempty"`
	Source        string    `json:"source"`
	ReceivedAt    time.Time `json:"received_at"`
}

type Facility struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	Address     string    `json:"address"`
	Phone       string    `json:"phone"`
	Capacity    int       `json:"capacity"`
	Occupied    int       `json:"occupied"`
	Latitude    *float64  `json:"latitude,omitempty"`
	Longitude   *float64  `json:"longitude,omitempty"`
	MapLabel    string    `json:"map_label,omitempty"`
	MarkerColor string    `json:"marker_color,omitempty"`
	Notes       string    `json:"notes"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Location struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Address    string    `json:"address"`
	City       string    `json:"city"`
	Region     string    `json:"region"`
	PostalCode string    `json:"postal_code"`
	Latitude   *float64  `json:"latitude,omitempty"`
	Longitude  *float64  `json:"longitude,omitempty"`
	Notes      string    `json:"notes"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type MapOverlay struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	FileName     string           `json:"file_name"`
	Color        string           `json:"color"`
	UseKMLStyles bool             `json:"use_kml_styles"`
	Opacity      int              `json:"opacity"`
	Visible      bool             `json:"visible"`
	Features     []OverlayFeature `json:"features"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

type OverlayFeature struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Color        string            `json:"color,omitempty"`
	GeometryType string            `json:"geometry_type"`
	Paths        [][]MapCoordinate `json:"paths"`
}

type MapCoordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Asset struct {
	ID             string     `json:"id"`
	Category       string     `json:"category"`
	Name           string     `json:"name"`
	Identifier     string     `json:"identifier"`
	Status         string     `json:"status"`
	Quantity       int        `json:"quantity"`
	Location       string     `json:"location"`
	Custodian      string     `json:"custodian"`
	MaintenanceDue *time.Time `json:"maintenance_due,omitempty"`
	Notes          string     `json:"notes"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type AssetInput struct {
	Category          string     `json:"category"`
	Name              string     `json:"name"`
	Identifier        string     `json:"identifier"`
	Status            string     `json:"status"`
	Quantity          int        `json:"quantity"`
	Location          string     `json:"location"`
	Custodian         string     `json:"custodian"`
	MaintenanceDue    *time.Time `json:"maintenance_due,omitempty"`
	Notes             string     `json:"notes"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type ScheduleItem struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	Type                 string    `json:"type"`
	Status               string    `json:"status"`
	StartAt              time.Time `json:"start_at"`
	EndAt                time.Time `json:"end_at"`
	Location             string    `json:"location"`
	AssignedResponderIDs []string  `json:"assigned_responder_ids"`
	Coordinator          string    `json:"coordinator"`
	Notes                string    `json:"notes"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type ScheduleItemInput struct {
	Title                string     `json:"title"`
	Type                 string     `json:"type"`
	Status               string     `json:"status"`
	StartAt              time.Time  `json:"start_at"`
	EndAt                time.Time  `json:"end_at"`
	Location             string     `json:"location"`
	AssignedResponderIDs []string   `json:"assigned_responder_ids"`
	Coordinator          string     `json:"coordinator"`
	Notes                string     `json:"notes"`
	ExpectedUpdatedAt    *time.Time `json:"expected_updated_at,omitempty"`
}

type Qualification struct {
	ID           string     `json:"id"`
	ResponderID  string     `json:"responder_id"`
	Category     string     `json:"category"`
	Name         string     `json:"name"`
	Provider     string     `json:"provider"`
	CredentialID string     `json:"credential_id"`
	Status       string     `json:"status"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Notes        string     `json:"notes"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type QualificationInput struct {
	ResponderID       string     `json:"responder_id"`
	Category          string     `json:"category"`
	Name              string     `json:"name"`
	Provider          string     `json:"provider"`
	CredentialID      string     `json:"credential_id"`
	Status            string     `json:"status"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	Notes             string     `json:"notes"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type OperationalMessage struct {
	ID         string    `json:"id"`
	Channel    string    `json:"channel"`
	IncidentID string    `json:"incident_id,omitempty"`
	Author     string    `json:"author"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type OperationalMessageInput struct {
	Channel    string `json:"channel"`
	IncidentID string `json:"incident_id"`
	Author     string `json:"author"`
	Body       string `json:"body"`
}

type Activity struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Summary    string    `json:"summary"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type IncidentInput struct {
	Title             string              `json:"title"`
	Type              string              `json:"type"`
	Severity          string              `json:"severity"`
	Status            string              `json:"status"`
	Address           string              `json:"address"`
	City              string              `json:"city"`
	Region            string              `json:"region"`
	PostalCode        string              `json:"postal_code"`
	Latitude          *float64            `json:"latitude"`
	Longitude         *float64            `json:"longitude"`
	ContactName       string              `json:"contact_name"`
	ContactPhone      string              `json:"contact_phone"`
	Description       string              `json:"description"`
	Command           *[]CommandRoleInput `json:"command,omitempty"`
	ExpectedUpdatedAt *time.Time          `json:"expected_updated_at,omitempty"`
}

type ResponderInput struct {
	Name              string     `json:"name"`
	Callsign          string     `json:"callsign"`
	Type              string     `json:"type"`
	Status            string     `json:"status"`
	Phone             string     `json:"phone"`
	Capabilities      []string   `json:"capabilities"`
	Latitude          *float64   `json:"latitude"`
	Longitude         *float64   `json:"longitude"`
	APRSEnabled       bool       `json:"aprs_enabled"`
	MapLabel          string     `json:"map_label"`
	MarkerColor       string     `json:"marker_color"`
	Notes             string     `json:"notes"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type ResponderPositionInput struct {
	Latitude          float64    `json:"latitude"`
	Longitude         float64    `json:"longitude"`
	AccuracyMeters    *float64   `json:"accuracy_meters,omitempty"`
	Source            string     `json:"source,omitempty"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type FacilityInput struct {
	Name              string     `json:"name"`
	Type              string     `json:"type"`
	Status            string     `json:"status"`
	Address           string     `json:"address"`
	Phone             string     `json:"phone"`
	Capacity          int        `json:"capacity"`
	Occupied          int        `json:"occupied"`
	Latitude          *float64   `json:"latitude"`
	Longitude         *float64   `json:"longitude"`
	MapLabel          string     `json:"map_label"`
	MarkerColor       string     `json:"marker_color"`
	Notes             string     `json:"notes"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type LocationInput struct {
	Name              string     `json:"name"`
	Type              string     `json:"type"`
	Address           string     `json:"address"`
	City              string     `json:"city"`
	Region            string     `json:"region"`
	PostalCode        string     `json:"postal_code"`
	Latitude          *float64   `json:"latitude"`
	Longitude         *float64   `json:"longitude"`
	Notes             string     `json:"notes"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type MapOverlayInput struct {
	Name         string           `json:"name"`
	FileName     string           `json:"file_name"`
	Color        string           `json:"color"`
	UseKMLStyles bool             `json:"use_kml_styles"`
	Opacity      int              `json:"opacity"`
	Visible      bool             `json:"visible"`
	Features     []OverlayFeature `json:"features"`
}

type MapOverlayUpdateInput struct {
	Name              string     `json:"name"`
	Color             string     `json:"color"`
	UseKMLStyles      bool       `json:"use_kml_styles"`
	Opacity           int        `json:"opacity"`
	Visible           bool       `json:"visible"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}
