package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *apiServer) connectionDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	items := s.networkConnectionChecks()
	settings := s.store.Snapshot().Settings
	items = append(items, s.aprsConnectionChecks(settings.APRS)...)

	type result struct {
		items []ConnectionCheck
	}
	results := make(chan result, 3)
	go func() { results <- result{items: s.weatherConnectionChecks(ctx, settings)} }()
	go func() { results <- result{items: s.waterConnectionChecks(ctx)} }()
	go func() { results <- result{items: s.integrationConnectionChecks(ctx, settings)} }()
	for range 3 {
		select {
		case value := <-results:
			items = append(items, value.items...)
		case <-ctx.Done():
			items = append(items, ConnectionCheck{ID: "provider-timeout", Label: "External providers", State: "disconnected", Detail: "Connection tests exceeded the twenty-second limit"})
			goto complete
		}
	}

complete:
	overall := "connected"
	for _, item := range items {
		if item.State == "disconnected" {
			overall = "attention"
			break
		}
		if item.State == "stale" && overall == "connected" {
			overall = "degraded"
		}
	}
	writeJSON(w, http.StatusOK, ConnectionDiagnostics{CheckedAt: time.Now().UTC(), Overall: overall, Items: items})
}

func (s *apiServer) networkConnectionChecks() []ConnectionCheck {
	if s.network == nil {
		return []ConnectionCheck{{ID: "local-app", Label: "Tickets Local", State: "connected", Detail: "Local application is responding"}}
	}
	status := s.network.Status()
	items := []ConnectionCheck{{ID: "local-app", Label: "Tickets Local", State: "connected", Detail: fmt.Sprintf("%s · port %d", labelNetworkMode(status.ActiveMode), status.ActivePort)}}
	switch status.ActiveMode {
	case networkModeHost:
		items = append(items, ConnectionCheck{ID: "lan-host", Label: "LAN Host", State: "connected", Detail: "Authoritative Host is accepting authenticated LAN requests"})
		if len(status.Clients) == 0 {
			items = append(items, ConnectionCheck{ID: "lan-clients", Label: "Client computers", State: "disabled", Detail: "No Client has checked in during the last five minutes"})
		}
		now := time.Now().UTC()
		for index, client := range status.Clients {
			state := "connected"
			detail := client.Address + " · checked in " + relativeConnectionAge(now, client.LastSeenAt)
			if now.Sub(client.LastSeenAt) > 30*time.Second {
				state = "stale"
				detail += " · not currently polling"
			}
			lastSeen := client.LastSeenAt
			items = append(items, ConnectionCheck{ID: fmt.Sprintf("lan-client-%d", index), Label: client.Name, State: state, Detail: detail, LastSeenAt: &lastSeen})
		}
	case networkModeClient:
		items = append(items, ConnectionCheck{ID: "lan-host", Label: "LAN Host", State: "connected", Detail: "This report was received from the authoritative Host"})
	default:
		items = append(items, ConnectionCheck{ID: "lan-sharing", Label: "LAN sharing", State: "disabled", Detail: "Standalone mode; no Host or Clients are configured"})
	}
	return items
}

func (s *apiServer) aprsConnectionChecks(settings APRSSettings) []ConnectionCheck {
	if !settings.Enabled || s.aprs == nil {
		return []ConnectionCheck{
			{ID: "aprs-is", Label: "APRS-IS", State: "disabled", Detail: "Internet APRS reception is off"},
			{ID: "aprs-local", Label: "Local RF / Dire Wolf", State: "disabled", Detail: "Local RF reception is off"},
		}
	}
	status := s.aprs.Status()
	internetEnabled := settings.Mode == "internet" || settings.Mode == "hybrid"
	localEnabled := settings.Mode == "local" || settings.Mode == "hybrid"
	return []ConnectionCheck{
		connectionCheckFromState("aprs-is", "APRS-IS", status.State, status.Message, internetEnabled, status.LastPacketAt),
		connectionCheckFromState("aprs-local", "Local RF / Dire Wolf", status.Local.State, status.Local.Message, localEnabled, status.Local.LastPacketAt),
	}
}

func (s *apiServer) weatherConnectionChecks(ctx context.Context, settings Settings) []ConnectionCheck {
	if s.weather == nil || !settings.Weather.Enabled {
		return []ConnectionCheck{
			{ID: "nws", Label: "NWS weather", State: "disabled", Detail: "Weather awareness is off"},
			{ID: "nws-radar", Label: "NWS radar", State: "disabled", Detail: "Radar is off"},
		}
	}
	status := s.weather.Status(ctx)
	items := []ConnectionCheck{connectionCheckFromState("nws", "NWS weather", status.State, status.Message, true, status.UpdatedAt)}
	if !settings.Weather.RadarEnabled {
		return append(items, ConnectionCheck{ID: "nws-radar", Label: "NWS radar", State: "disabled", Detail: "Radar layer is off"})
	}
	_, updatedAt, stale, err := s.weather.Radar(ctx, url.Values{"bbox": {"-1000000,-1000000,1000000,1000000"}, "width": {"128"}, "height": {"128"}})
	if err != nil {
		return append(items, ConnectionCheck{ID: "nws-radar", Label: "NWS radar", State: "disconnected", Detail: err.Error()})
	}
	state := "connected"
	if stale {
		state = "stale"
	}
	return append(items, ConnectionCheck{ID: "nws-radar", Label: "NWS radar", State: state, Detail: "Radar image provider responded", LastSeenAt: &updatedAt})
}

func (s *apiServer) waterConnectionChecks(ctx context.Context) []ConnectionCheck {
	if s.water == nil {
		return []ConnectionCheck{{ID: "water", Label: "USGS / NOAA water", State: "disabled", Detail: "Water monitoring is unavailable"}}
	}
	status := s.water.Status(ctx)
	return []ConnectionCheck{connectionCheckFromState("water", "USGS / NOAA water", status.State, status.Message, status.State != "disabled", status.UpdatedAt)}
}

func (s *apiServer) integrationConnectionChecks(ctx context.Context, settings Settings) []ConnectionCheck {
	if s.integrations == nil || !settings.Integrations.Enabled {
		return []ConnectionCheck{
			{ID: "storm-reports", Label: "Storm reports", State: "disabled", Detail: "Operational feeds are off"},
			{ID: "infrastructure", Label: "Roads / infrastructure", State: "disabled", Detail: "Operational feeds are off"},
			{ID: "amateur-repeaters", Label: "Amateur repeaters", State: "disabled", Detail: "Operational feeds are off"},
			{ID: "gmrs-repeaters", Label: "GMRS repeaters", State: "disabled", Detail: "Operational feeds are off"},
			{ID: "meshcore", Label: "MeshCore nodes", State: "disabled", Detail: "Operational feeds are off"},
			{ID: "aredn", Label: "AREDN mesh nodes", State: "disabled", Detail: "Operational feeds are off"},
			{ID: "sensors", Label: "Local sensors", State: "disabled", Detail: "Operational feeds are off"},
		}
	}
	status := s.integrations.Status(ctx)
	items := []ConnectionCheck{
		integrationFeedCheck("storm-reports", "Storm reports", settings.Integrations.StormReportsURL, len(status.StormReports), status, false),
		integrationFeedCheck("infrastructure", "Roads / infrastructure", settings.Integrations.InfrastructureURL, len(status.Infrastructure), status, false),
		integrationFeedCheck("amateur-repeaters", "Amateur repeaters", defaultString(settings.Integrations.AmateurRepeatersURL, enabledEndpoint(settings.Integrations.AmateurOSMEnabled, "OpenStreetMap")), len(status.AmateurRepeaters), status, true),
		integrationFeedCheck("gmrs-repeaters", "GMRS repeaters", settings.Integrations.GMRSRepeatersURL, len(status.GMRSRepeaters), status, true),
		integrationFeedCheck("meshcore", "MeshCore nodes", settings.Integrations.MeshCoreURL, len(status.MeshCoreNodes), status, true),
		integrationFeedCheck("sensors", "Local sensors", settings.Integrations.SensorURLs, len(status.Sensors), status, false),
	}
	if strings.TrimSpace(settings.Integrations.AREDNNodeURLs) == "" {
		items = append(items, ConnectionCheck{ID: "aredn", Label: "AREDN mesh nodes", State: "disabled", Detail: "No AREDN node status URLs are configured"})
	} else {
		for index, node := range status.AREDNNodes {
			state := "disconnected"
			if node.Reachable {
				state = "connected"
			}
			checked := node.CheckedAt
			items = append(items, ConnectionCheck{ID: fmt.Sprintf("aredn-%d", index), Label: defaultString(node.Name, "AREDN node"), State: state, Detail: node.Message, LastSeenAt: &checked})
		}
		if len(status.AREDNNodes) == 0 {
			items = append(items, ConnectionCheck{ID: "aredn", Label: "AREDN nodes", State: "disconnected", Detail: "Configured nodes did not return status"})
		}
	}
	return items
}

func enabledEndpoint(enabled bool, label string) string {
	if enabled {
		return label
	}
	return ""
}

func integrationFeedCheck(id, label, configured string, itemCount int, status IntegrationStatus, requireItems bool) ConnectionCheck {
	if strings.TrimSpace(configured) == "" {
		return ConnectionCheck{ID: id, Label: label, State: "disabled", Detail: "No endpoint is configured"}
	}
	state := "connected"
	if status.State == "stale" {
		state = "stale"
	} else if status.State == "partial" && itemCount == 0 {
		state = "disconnected"
	} else if requireItems && itemCount == 0 {
		state = "stale"
	}
	detail := fmt.Sprintf("Provider responded · %d item(s)", itemCount)
	if requireItems && itemCount == 0 && status.State != "partial" && status.State != "stale" {
		detail = "Provider responded, but no map data was returned"
	} else if state != "connected" {
		detail = status.Message
	}
	return ConnectionCheck{ID: id, Label: label, State: state, Detail: detail, LastSeenAt: status.UpdatedAt}
}

func connectionCheckFromState(id, label, sourceState, detail string, enabled bool, lastSeen *time.Time) ConnectionCheck {
	if !enabled {
		return ConnectionCheck{ID: id, Label: label, State: "disabled", Detail: detail}
	}
	state := "disconnected"
	switch sourceState {
	case "connected", "current", "running":
		state = "connected"
	case "stale", "partial", "waiting", "connecting", "authenticating", "reconnecting":
		state = "stale"
	case "disabled":
		state = "disabled"
	}
	return ConnectionCheck{ID: id, Label: label, State: state, Detail: detail, LastSeenAt: lastSeen}
}

func labelNetworkMode(mode string) string {
	switch mode {
	case networkModeHost:
		return "Host mode"
	case networkModeClient:
		return "Client mode"
	default:
		return "Standalone mode"
	}
}

func relativeConnectionAge(now, value time.Time) string {
	age := now.Sub(value)
	if age < time.Second {
		return "just now"
	}
	if age < time.Minute {
		return fmt.Sprintf("%d seconds ago", int(age.Seconds()))
	}
	return fmt.Sprintf("%d minutes ago", int(age.Minutes()))
}
