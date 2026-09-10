package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"
)

func TestAPRSManagerVerifiedLoginFilterAndPosition(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	loginLine := make(chan string, 1)
	serverError := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverError <- err
			return
		}
		defer conn.Close()
		if _, err = io.WriteString(conn, "# aprsc test server\r\n"); err != nil {
			serverError <- err
			return
		}
		reader := bufio.NewReader(conn)
		login, err := reader.ReadString('\n')
		if err != nil {
			serverError <- err
			return
		}
		loginLine <- strings.TrimSpace(login)
		_, err = io.WriteString(conn, "# logresp N0CALL verified, server TEST\r\nN0CALL-7>APRS:!3503.50N/08640.25W>090/010 Test tracker\r\nN1AREA-9>APRS:!3503.60N/08640.30W>Nearby station\r\n")
		if err != nil {
			serverError <- err
			return
		}
		<-time.After(3 * time.Second)
	}()

	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateResponder(ResponderInput{
		Name: "Tracker", Callsign: "N0CALL-7", Status: "available", APRSEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveAPRSPasscode("12345"); err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.Server = listener.Addr().String()
	settings.CenterLat = 35.0583
	settings.CenterLon = -86.6708
	settings.APRS.AreaEnabled = true
	settings.APRS.AreaRadiusMiles = 25
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	manager.Start(ctx)
	defer func() {
		cancel()
		manager.Stop()
	}()

	select {
	case login := <-loginLine:
		if !strings.Contains(login, "user N0CALL pass 12345 vers TicketsLocal "+version) {
			t.Fatalf("unexpected APRS-IS login: %q", login)
		}
		if !strings.Contains(login, "filter b/N0CALL-7") {
			t.Fatalf("login did not contain tracked responder filter: %q", login)
		}
		if !strings.Contains(login, "r/35.058300/-86.670800/40.2") {
			t.Fatalf("login did not contain the configured area filter: %q", login)
		}
	case err := <-serverError:
		t.Fatalf("fake APRS-IS server: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for APRS-IS login")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := store.Snapshot()
		activity := manager.Activity()
		if len(snapshot.Tracks) == 1 && snapshot.Responders[0].PositionUpdatedAt != nil && len(activity) == 2 {
			status := manager.Status()
			if status.PositionsUpdated != 1 || status.PositionPacketsDecoded != 2 ||
				status.AreaPositionsReceived != 2 || status.AreaStations != 2 ||
				status.State != "connected" || !status.LoginVerified ||
				status.LoginVerifiedAt == nil {
				t.Fatalf("unexpected APRS status: %+v", status)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("APRS position was not saved")
}

func TestAPRSManagerAreaOnlyConfiguration(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	if err = store.SaveAPRSPasscode("12345"); err != nil {
		t.Fatal(err)
	}
	settings.APRS.Enabled = true
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.AreaEnabled = true
	settings.APRS.AreaRadiusMiles = 10
	settings.CenterLat = 36.1627
	settings.CenterLon = -86.7816
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	config := manager.config()
	if !config.enabled {
		t.Fatal("area-only APRS configuration did not enable the receiver")
	}
	if config.filter != "r/36.162700/-86.781600/16.1" {
		t.Fatalf("area filter = %q, want range filter", config.filter)
	}
}

func TestValidateAPRSISLoginRejectsUnverifiedResponse(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		_, _ = io.WriteString(server, "# logresp N0CALL unverified, server TEST\r\n")
	}()
	scanner := bufio.NewScanner(client)
	err := validateAPRSISLogin(scanner, client, "N0CALL")
	if !errors.Is(err, errAPRSLoginRejected) {
		t.Fatalf("login validation error = %v, want rejected", err)
	}
}

func TestAPRSDistanceCalculationForAreaFilter(t *testing.T) {
	if distanceKilometers(35, -86, 35.1, -86) < 10 {
		t.Fatal("distance calculation is unexpectedly short")
	}
	if distanceKilometers(35, -86, 35.01, -86) > 2 {
		t.Fatal("distance calculation is unexpectedly long")
	}
}

func TestAPRSDuplicateKeyIgnoresNetworkPathChanges(t *testing.T) {
	rf := "N0CALL-7>APRS,WIDE1-1:!3503.50N/08640.25W>Tracker"
	internet := "N0CALL-7>APRS,WIDE1-1,qAR,N0IGATE:!3503.50N/08640.25W>Tracker"
	if aprsDuplicateKey(rf) != aprsDuplicateKey(internet) {
		t.Fatalf("RF and APRS-IS copies did not receive the same duplicate key")
	}
}

func TestLocalIGateUsesVerifiedMinimalAPRSISConnection(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAPRSPasscode("12345"); err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.Mode = "local"
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.Local.IGateEnabled = true
	settings.APRS.Local.ShowAll = true
	settings.APRS.Local.AudioDevice = "Radio Input"
	settings.APRS.Local.AudioOutputDevice = "Radio Output"
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	internet := manager.config()
	if !internet.enabled || !internet.validationOnly || internet.filter != "b/N0CALL" || len(internet.callsign) != 0 {
		t.Fatalf("unexpected local iGate validation config: %+v", internet)
	}
	if manager.localConfig().enabled {
		t.Fatal("local iGate started before APRS-IS credentials were verified")
	}
	manager.updateStatus(func(status *APRSStatus) { status.LoginVerified = true })
	if !manager.localConfig().enabled {
		t.Fatal("local iGate did not enable after APRS-IS credentials were verified")
	}
}
