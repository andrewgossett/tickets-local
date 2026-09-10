package main

import (
	"encoding/csv"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func exportTestIncident() Incident {
	stamp := time.Date(2026, time.September, 8, 15, 4, 5, 0, time.UTC)
	return Incident{
		ID: "incident-1", Number: 42, Title: "Flood, North Fork", Type: "Flood", Severity: "high", Status: "assigned",
		Address: "1 River Rd", City: "Nolensville", Region: "TN", PostalCode: "37135", Description: "Road closed\nUse east approach",
		CreatedAt: stamp.Add(-time.Hour), UpdatedAt: stamp,
		Assignments: []Assignment{{ResponderID: "unit-1", AssignedAt: stamp.Add(-30 * time.Minute)}},
		Actions:     []Action{{ID: "action-1", Description: "Called, command\nconfirmed", CreatedAt: stamp}},
		Attachments: []IncidentAttachment{{ID: "att-1", Name: "site-plan.pdf", MediaType: "application/pdf", Size: 1234, StorageName: "secret-disk-name.pdf"}},
	}
}

func TestBuildIncidentExportFormatsAndSafety(t *testing.T) {
	incident := exportTestIncident()
	settings := Settings{Organization: "County EOC"}
	for _, format := range []string{"winlink", "ics213", "ics214", "ics309"} {
		result, err := BuildIncidentExport(incident, settings, format)
		if err != nil {
			t.Fatalf("%s export: %v", format, err)
		}
		if result.Content == "" || !strings.Contains(result.Content, "Flood") || !strings.Contains(result.Filename, format) {
			t.Fatalf("incomplete %s export: %+v", format, result)
		}
		if strings.Contains(result.Content, "secret-disk-name") {
			t.Fatalf("%s export exposed an internal attachment storage name", format)
		}
	}
	winlink, _ := BuildIncidentExport(incident, settings, "winlink")
	if !strings.Contains(winlink.Content, "site-plan.pdf") || strings.Contains(winlink.Content, "Road closed\r\nUse") {
		t.Fatalf("Winlink content was not sanitized or omitted attachment metadata: %q", winlink.Content)
	}
	if _, err := BuildIncidentExport(incident, settings, "unknown"); err == nil {
		t.Fatal("unknown export format was accepted")
	}
}

func TestICSCSVExportsAreParseable(t *testing.T) {
	incident := exportTestIncident()
	incident.Actions = append(incident.Actions, Action{Description: "=HYPERLINK(\"https://example.invalid\")", CreatedAt: incident.UpdatedAt})
	for _, format := range []string{"ics214", "ics309"} {
		result, err := BuildIncidentExport(incident, Settings{Organization: "County EOC"}, format)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := csv.NewReader(strings.NewReader(result.Content)).ReadAll()
		if err != nil || len(rows) != 6 {
			t.Fatalf("%s CSV invalid: rows=%v err=%v", format, rows, err)
		}
		if !strings.Contains(rows[4][len(rows[4])-1], "Called, command confirmed") {
			t.Fatalf("%s CSV did not preserve escaped activity: %#v", format, rows[4])
		}
		if !strings.HasPrefix(rows[5][len(rows[5])-1], "'=") {
			t.Fatalf("%s export did not neutralize a CSV formula: %#v", format, rows[5])
		}
	}
}

func TestBuildWinlinkEmail(t *testing.T) {
	incident := exportTestIncident()
	result, err := BuildWinlinkEmail(incident, WinlinkEmailInput{
		Recipient: "N0CALL@winlink.org", Precedence: "R", Subject: "Shelter status", Body: "Reviewed\nmessage",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ContentType != "message/rfc822" || !strings.HasSuffix(result.Filename, ".eml") {
		t.Fatalf("unexpected email metadata: %+v", result)
	}
	if !strings.Contains(result.Content, "To: N0CALL@winlink.org\r\n") || !strings.Contains(result.Content, "//WL2K R/Shelter status") || !strings.Contains(result.Content, "Reviewed\r\nmessage") {
		t.Fatalf("unexpected Winlink email: %q", result.Content)
	}
	for _, input := range []WinlinkEmailInput{
		{Recipient: "not-an-address", Precedence: "R", Subject: "Test", Body: "Body"},
		{Recipient: "N0CALL@winlink.org", Precedence: "X", Subject: "Test", Body: "Body"},
		{Recipient: "N0CALL@winlink.org", Precedence: "R", Subject: "//WL2K R/Test", Body: "Body"},
		{Recipient: "N0CALL@winlink.org", Precedence: "R", Subject: "Test", Body: ""},
	} {
		if _, err := BuildWinlinkEmail(incident, input); err == nil {
			t.Fatalf("invalid Winlink email was accepted: %+v", input)
		}
	}
}

func TestIncidentExportAPI(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	incident, err := store.CreateIncident(IncidentInput{Title: "Radio check", Type: "Communications", Severity: "medium", Status: "new", Address: "EOC"})
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := newAPIServer(store, webAssets, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()
	result := requestJSON[IncidentExport](t, server.URL+"/api/incidents/"+incident.ID+"/export?format=ics213", http.MethodGet, nil, http.StatusOK)
	if result.Format != "ics213" || !strings.Contains(result.Content, "Radio check") {
		t.Fatalf("unexpected export response: %+v", result)
	}
	email := requestJSON[IncidentExport](t, server.URL+"/api/incidents/"+incident.ID+"/winlink-email", http.MethodPost, WinlinkEmailInput{
		Recipient: "N0CALL@winlink.org", Precedence: "P", Subject: "Radio check", Body: result.Content,
	}, http.StatusOK)
	if !strings.Contains(email.Content, "//WL2K P/Radio check") {
		t.Fatalf("Winlink email prefix missing: %+v", email)
	}
	response, err := http.Get(server.URL + "/api/incidents/missing/export?format=ics213")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing incident status = %d, want 404", response.StatusCode)
	}
}
