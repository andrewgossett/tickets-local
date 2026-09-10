package main

import (
	"errors"
	"testing"
	"time"
)

func TestScheduleLifecycleAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)
	item, err := store.CreateScheduleItem(ScheduleItemInput{Title: "Day operations shift", Type: "shift", Status: "planned", StartAt: start, EndAt: end, Location: "EOC"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = store.UpdateScheduleItem(item.ID, ScheduleItemInput{Title: item.Title, Type: item.Type, Status: "confirmed", StartAt: start, EndAt: end, Location: item.Location, ExpectedUpdatedAt: &item.UpdatedAt})
	if err != nil || item.Status != "confirmed" {
		t.Fatalf("schedule update failed: %+v %v", item, err)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || len(replayed.Snapshot().Schedule) != 1 || replayed.Snapshot().Schedule[0].Status != "confirmed" {
		t.Fatalf("schedule replay failed: %+v %v", replayed.Snapshot().Schedule, err)
	}
	if err := replayed.DeleteScheduleItem(item.ID); err != nil || len(replayed.Snapshot().Schedule) != 0 {
		t.Fatalf("schedule deletion failed: %v", err)
	}
}

func TestScheduleValidationConflictAndAssignments(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	start := time.Now().UTC().Add(time.Hour)
	if _, err := store.CreateScheduleItem(ScheduleItemInput{Title: "Invalid", Type: "shift", StartAt: start, EndAt: start}); err == nil {
		t.Fatal("expected time validation")
	}
	if _, err := store.CreateScheduleItem(ScheduleItemInput{Title: "Invalid assignment", Type: "training", StartAt: start, EndAt: start.Add(time.Hour), AssignedResponderIDs: []string{"missing"}}); err == nil {
		t.Fatal("expected responder validation")
	}
	item, err := store.CreateScheduleItem(ScheduleItemInput{Title: "Radio exercise", Type: "exercise", StartAt: start, EndAt: start.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	stale := item.UpdatedAt.Add(-time.Second)
	_, err = store.UpdateScheduleItem(item.ID, ScheduleItemInput{Title: item.Title, Type: item.Type, StartAt: start, EndAt: start.Add(2 * time.Hour), ExpectedUpdatedAt: &stale})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}
