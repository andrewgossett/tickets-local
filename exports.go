package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"
)

const maxExportContentBytes = 2 << 20

type IncidentExport struct {
	Format      string `json:"format"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
}

type WinlinkEmailInput struct {
	Recipient  string `json:"recipient"`
	Precedence string `json:"precedence"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
}

func BuildIncidentExport(incident Incident, settings Settings, format string) (IncidentExport, error) {
	format = strings.ToLower(clean(format))
	result := IncidentExport{Format: format, ContentType: "text/plain; charset=utf-8"}
	switch format {
	case "winlink":
		result.Filename = fmt.Sprintf("incident-%d-winlink.txt", incident.Number)
		result.Content = renderWinlinkIncident(incident, settings)
	case "ics213":
		result.Filename = fmt.Sprintf("incident-%d-ics213.txt", incident.Number)
		result.Content = renderICS213(incident, settings)
	case "ics214":
		result.Filename = fmt.Sprintf("incident-%d-ics214.csv", incident.Number)
		result.ContentType = "text/csv; charset=utf-8"
		result.Content = renderICS214(incident, settings)
	case "ics309":
		result.Filename = fmt.Sprintf("incident-%d-ics309.csv", incident.Number)
		result.ContentType = "text/csv; charset=utf-8"
		result.Content = renderICS309(incident, settings)
	default:
		return IncidentExport{}, validationError{"export format must be winlink, ics213, ics214, or ics309"}
	}
	if len(result.Content) > maxExportContentBytes {
		return IncidentExport{}, validationError{"export exceeds the 2 MB safety limit"}
	}
	return result, nil
}

func BuildWinlinkEmail(incident Incident, input WinlinkEmailInput) (IncidentExport, error) {
	recipient := safeExportText(input.Recipient)
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient || !strings.Contains(recipient, "@") {
		return IncidentExport{}, validationError{"enter one complete recipient email address, such as CALLSIGN@winlink.org"}
	}
	precedence := strings.ToUpper(clean(input.Precedence))
	if !strings.Contains("RPOZ", precedence) || len(precedence) != 1 {
		return IncidentExport{}, validationError{"Winlink precedence must be R, P, O, or Z"}
	}
	subject := safeExportText(input.Subject)
	if subject == "" {
		return IncidentExport{}, validationError{"email subject is required"}
	}
	if strings.HasPrefix(strings.ToUpper(subject), "//WL2K") {
		return IncidentExport{}, validationError{"enter the subject without the //WL2K prefix; Tickets Local adds it automatically"}
	}
	if len(subject) > 100 {
		return IncidentExport{}, validationError{"email subject must be 100 characters or fewer"}
	}
	body := normalizeEmailBody(input.Body)
	if strings.TrimSpace(body) == "" {
		return IncidentExport{}, validationError{"email body is required"}
	}
	if len(body) > maxExportContentBytes {
		return IncidentExport{}, validationError{"email body exceeds the 2 MB safety limit"}
	}
	fullSubject := fmt.Sprintf("//WL2K %s/%s", precedence, subject)
	content := "To: " + recipient + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", fullSubject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + body
	return IncidentExport{
		Format:      "winlink-email",
		Filename:    fmt.Sprintf("incident-%d-winlink-email.eml", incident.Number),
		ContentType: "message/rfc822",
		Content:     content,
	}, nil
}

func renderWinlinkIncident(i Incident, settings Settings) string {
	var b strings.Builder
	writeLine := func(label, value string) { fmt.Fprintf(&b, "%s: %s\r\n", label, safeExportText(value)) }
	writeLine("FORM", "TICKETS LOCAL INCIDENT")
	writeLine("ORGANIZATION", settings.Organization)
	writeLine("INCIDENT", fmt.Sprintf("#%d %s", i.Number, i.Title))
	writeLine("TYPE", i.Type)
	writeLine("PRIORITY", i.Severity)
	writeLine("STATUS", i.Status)
	writeLine("LOCATION", exportAddress(i))
	writeLine("CONTACT", strings.TrimSpace(i.ContactName+" "+i.ContactPhone))
	writeLine("CREATED", exportTime(i.CreatedAt))
	writeLine("UPDATED", exportTime(i.UpdatedAt))
	b.WriteString("\r\nDESCRIPTION:\r\n" + safeExportText(i.Description) + "\r\n\r\nASSIGNMENTS:\r\n")
	for _, assignment := range i.Assignments {
		writeLine("UNIT", assignment.ResponderID+" assigned "+exportTime(assignment.AssignedAt))
	}
	b.WriteString("\r\nACTIVITY:\r\n")
	for _, action := range i.Actions {
		writeLine(exportTime(action.CreatedAt), action.Description)
	}
	b.WriteString("\r\nATTACHMENT REFERENCES:\r\n")
	for _, attachment := range i.Attachments {
		writeLine("FILE", fmt.Sprintf("%s (%s, %d bytes)", attachment.Name, attachment.MediaType, attachment.Size))
	}
	return b.String()
}

func renderICS213(i Incident, settings Settings) string {
	return fmt.Sprintf("ICS-213 GENERAL MESSAGE\r\nINCIDENT NAME: %s\r\nTO: Incident Command\r\nFROM: %s\r\nSUBJECT: Incident #%d - %s\r\nDATE/TIME: %s\r\n\r\nMESSAGE:\r\n%s\r\n\r\nREPLY:\r\n\r\n\r\nAPPROVED BY: ____________________\r\nPOSITION: ____________________\r\n",
		safeExportText(i.Title), safeExportText(settings.Organization), i.Number, safeExportText(i.Type), exportTime(i.UpdatedAt), safeExportText(i.Description))
}

func renderICS214(i Incident, settings Settings) string {
	rows := [][]string{{"ICS-214 Activity Log", ""}, {"Incident", fmt.Sprintf("#%d %s", i.Number, safeCSVText(i.Title))}, {"Organization", safeCSVText(settings.Organization)}, {"Date/Time", "Activity"}}
	for _, action := range i.Actions {
		rows = append(rows, []string{exportTime(action.CreatedAt), safeCSVText(action.Description)})
	}
	return encodeCSV(rows)
}

func renderICS309(i Incident, settings Settings) string {
	rows := [][]string{{"ICS-309 Communications Log", "", "", ""}, {"Incident", fmt.Sprintf("#%d %s", i.Number, safeCSVText(i.Title)), "", ""}, {"Operator/Organization", safeCSVText(settings.Organization), "", ""}, {"Date/Time", "From", "To", "Message"}}
	for _, action := range i.Actions {
		rows = append(rows, []string{exportTime(action.CreatedAt), safeCSVText(settings.Organization), "Incident Command", safeCSVText(action.Description)})
	}
	return encodeCSV(rows)
}

func encodeCSV(rows [][]string) string {
	var b bytes.Buffer
	writer := csv.NewWriter(&b)
	writer.UseCRLF = true
	for _, row := range rows {
		_ = writer.Write(row)
	}
	writer.Flush()
	return b.String()
}

func exportAddress(i Incident) string {
	parts := make([]string, 0, 4)
	for _, value := range []string{i.Address, i.City, i.Region, i.PostalCode} {
		if value = safeExportText(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

func exportTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func safeExportText(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value))
}

func safeCSVText(value string) string {
	value = safeExportText(value)
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func normalizeEmailBody(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 {
			return r
		}
		return -1
	}, value)
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func findIncidentExport(state State, id, format string) (IncidentExport, error) {
	for _, incident := range state.Incidents {
		if incident.ID == id {
			return BuildIncidentExport(incident, state.Settings, format)
		}
	}
	return IncidentExport{}, ErrNotFound
}

func findWinlinkEmail(state State, id string, input WinlinkEmailInput) (IncidentExport, error) {
	for _, incident := range state.Incidents {
		if incident.ID == id {
			return BuildWinlinkEmail(incident, input)
		}
	}
	return IncidentExport{}, ErrNotFound
}
