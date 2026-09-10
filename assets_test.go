package main

import (
	"errors"
	"testing"
	"time"
)

func TestAssetLifecycleAndReplay(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	asset, err := store.CreateAsset(AssetInput{Category: "vehicle", Name: "Command 1", Identifier: "C1", Status: "ready", Quantity: 1, Location: "Station 1", MaintenanceDue: &due})
	if err != nil {
		t.Fatal(err)
	}
	asset, err = store.UpdateAsset(asset.ID, AssetInput{Category: "vehicle", Name: asset.Name, Identifier: asset.Identifier, Status: "assigned", Quantity: 1, Location: "Incident 1001", Custodian: "Operations", ExpectedUpdatedAt: &asset.UpdatedAt})
	if err != nil || asset.Status != "assigned" {
		t.Fatalf("asset update failed: %+v %v", asset, err)
	}
	replayed, err := OpenStore(dataDir)
	if err != nil || len(replayed.Snapshot().Assets) != 1 || replayed.Snapshot().Assets[0].Status != "assigned" {
		t.Fatalf("asset replay failed: %+v %v", replayed.Snapshot().Assets, err)
	}
	if err := replayed.DeleteAsset(asset.ID); err != nil || len(replayed.Snapshot().Assets) != 0 {
		t.Fatalf("asset deletion failed: %v", err)
	}
}

func TestAssetValidationAndConflict(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	if _, err := store.CreateAsset(AssetInput{Category: "unknown", Name: "Bad", Quantity: 1}); err == nil {
		t.Fatal("expected category validation")
	}
	asset, _ := store.CreateAsset(AssetInput{Category: "equipment", Name: "Radios", Status: "ready", Quantity: 10})
	stale := asset.UpdatedAt.Add(-time.Second)
	if _, err := store.UpdateAsset(asset.ID, AssetInput{Category: "equipment", Name: "Radios", Status: "ready", Quantity: 10, ExpectedUpdatedAt: &stale}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}
