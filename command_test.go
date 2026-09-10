package main

import (
	"testing"
	"time"
)

func TestIncidentCommandStructureLifecycleAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	commander, _ := store.CreateResponder(ResponderInput{Name: "Incident Commander", Status: "available"})
	operations, _ := store.CreateResponder(ResponderInput{Name: "Operations Chief", Status: "available"})
	command := []CommandRoleInput{{Role: "incident_commander", ResponderID: commander.ID}, {Role: "operations", ResponderID: operations.ID}}
	incident, err := store.CreateIncident(IncidentInput{Title: "Search operation", Type: "Search & Rescue", Severity: "high", Status: "new", Address: "Staging", Command: &command})
	if err != nil || len(incident.Command) != 2 {
		t.Fatalf("command creation failed: %+v %v", incident.Command, err)
	}
	assignedAt := incident.Command[0].AssignedAt
	command = command[:1]
	incident, err = store.UpdateIncident(incident.ID, IncidentInput{Title: incident.Title, Type: incident.Type, Severity: incident.Severity, Status: incident.Status, Address: incident.Address, Command: &command, ExpectedUpdatedAt: &incident.UpdatedAt})
	if err != nil || len(incident.Command) != 1 || !incident.Command[0].AssignedAt.Equal(assignedAt) {
		t.Fatalf("command update failed: %+v %v", incident.Command, err)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || len(replayed.Snapshot().Incidents[0].Command) != 1 {
		t.Fatalf("command replay failed: %+v %v", replayed.Snapshot().Incidents, err)
	}
}

func TestIncidentCommandValidation(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	responder, _ := store.CreateResponder(ResponderInput{Name: "Commander", Status: "available"})
	duplicate := []CommandRoleInput{{Role: "incident_commander", ResponderID: responder.ID}, {Role: "incident_commander", ResponderID: responder.ID}}
	_, err := store.CreateIncident(IncidentInput{Title: "Bad command", Type: "Other", Severity: "low", Address: "Here", Command: &duplicate})
	if err == nil {
		t.Fatal("expected duplicate command validation")
	}
	missing := []CommandRoleInput{{Role: "planning", ResponderID: "missing"}}
	_, err = store.CreateIncident(IncidentInput{Title: "Bad responder", Type: "Other", Severity: "low", Address: "Here", Command: &missing})
	if err == nil {
		t.Fatal("expected missing responder validation")
	}
}

func TestIncidentUpdateWithoutCommandPreservesStructure(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	responder, _ := store.CreateResponder(ResponderInput{Name: "Commander", Status: "available"})
	command := []CommandRoleInput{{Role: "incident_commander", ResponderID: responder.ID}}
	incident, _ := store.CreateIncident(IncidentInput{Title: "Operation", Type: "Other", Severity: "medium", Status: "new", Address: "Here", Command: &command})
	time.Sleep(time.Millisecond)
	updated, err := store.UpdateIncident(incident.ID, IncidentInput{Title: "Operation updated", Type: incident.Type, Severity: incident.Severity, Status: incident.Status, Address: incident.Address, ExpectedUpdatedAt: &incident.UpdatedAt})
	if err != nil || len(updated.Command) != 1 {
		t.Fatalf("command was not preserved: %+v %v", updated.Command, err)
	}
}
