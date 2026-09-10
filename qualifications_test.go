package main

import (
	"errors"
	"testing"
	"time"
)

func TestQualificationLifecycleReplayAndConflict(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	responder, err := store.CreateResponder(ResponderInput{Name: "Alex Morgan", Callsign: "N0CALL", Type: "Radio operator", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	completed := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	expires := completed.AddDate(2, 0, 0)
	item, err := store.CreateQualification(QualificationInput{ResponderID: responder.ID, Category: "certification", Name: "CPR/AED", Provider: "Red Cross", Status: "current", CompletedAt: &completed, ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	stale := item.UpdatedAt.Add(-time.Second)
	if _, err := store.UpdateQualification(item.ID, QualificationInput{ResponderID: responder.ID, Category: item.Category, Name: item.Name, Status: "current", CompletedAt: &completed, ExpiresAt: &expires, ExpectedUpdatedAt: &stale}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	item, err = store.UpdateQualification(item.ID, QualificationInput{ResponderID: responder.ID, Category: item.Category, Name: item.Name, Provider: item.Provider, Status: "current", CompletedAt: &completed, ExpiresAt: &expires, Notes: "Verified", ExpectedUpdatedAt: &item.UpdatedAt})
	if err != nil || item.Notes != "Verified" {
		t.Fatalf("update failed: %+v %v", item, err)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || len(replayed.Snapshot().Qualifications) != 1 || replayed.Snapshot().Qualifications[0].ResponderID != responder.ID {
		t.Fatalf("replay failed: %+v %v", replayed.Snapshot().Qualifications, err)
	}
	if err := replayed.DeleteQualification(item.ID); err != nil || len(replayed.Snapshot().Qualifications) != 0 {
		t.Fatalf("delete failed: %v", err)
	}
}

func TestQualificationValidation(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	if _, err := store.CreateQualification(QualificationInput{ResponderID: "missing", Category: "course", Name: "ICS-100"}); err == nil {
		t.Fatal("expected missing responder validation")
	}
	responder, _ := store.CreateResponder(ResponderInput{Name: "Operator", Status: "available"})
	if _, err := store.CreateQualification(QualificationInput{ResponderID: responder.ID, Category: "bad", Name: "Bad"}); err == nil {
		t.Fatal("expected category validation")
	}
	if _, err := store.CreateQualification(QualificationInput{ResponderID: responder.ID, Category: "license", Name: "License", CompletedAt: &now, ExpiresAt: &past}); err == nil {
		t.Fatal("expected date validation")
	}
}
