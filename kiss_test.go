package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKISSFrameAndAX25Decode(t *testing.T) {
	ax25 := testAX25UIFrame(
		"N0CALL-7",
		"APRS",
		[]testAX25Path{{callsign: "WIDE1-1", repeated: true}},
		"!3503.50N/08640.25W>Local receiver",
	)
	kiss := []byte{kissFEND, 0x00}
	for _, value := range ax25 {
		switch value {
		case kissFEND:
			kiss = append(kiss, kissFESC, kissTFEND)
		case kissFESC:
			kiss = append(kiss, kissFESC, kissTFESC)
		default:
			kiss = append(kiss, value)
		}
	}
	kiss = append(kiss, kissFEND)

	var decoded []byte
	err := readKISSFrames(bytes.NewReader(kiss), func(frame []byte) {
		decoded = frame
	})
	if err != io.EOF {
		t.Fatalf("readKISSFrames error = %v, want EOF", err)
	}
	if !bytes.Equal(decoded, ax25) {
		t.Fatalf("decoded AX.25 frame does not match: %x != %x", decoded, ax25)
	}
	line, err := ax25UIToTNC2(decoded)
	if err != nil {
		t.Fatal(err)
	}
	want := "N0CALL-7>APRS,WIDE1-1*:!3503.50N/08640.25W>Local receiver"
	if line != want {
		t.Fatalf("TNC2 packet = %q, want %q", line, want)
	}
}

func TestKISSTrailingLineTerminatorIsAccepted(t *testing.T) {
	frame := testAX25UIFrame(
		"KN4EIG-9",
		"S5TY6X",
		[]testAX25Path{{callsign: "W4ETR-10", repeated: true}, {callsign: "WIDE2-1"}},
		"`o>Pl ->/`\"7(}146.850MHz 146.520_2\r",
	)
	line, err := ax25UIToTNC2(frame)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(line, "\r") {
		t.Fatalf("TNC2 packet retained trailing carriage return: %q", line)
	}
	position, err := parseAPRSPacket(line, time.Date(2026, 9, 11, 18, 16, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if position.Callsign != "KN4EIG-9" || math.Abs(position.Latitude-35.828) > 0.00002 ||
		math.Abs(position.Longitude-(-83.575333)) > 0.00002 {
		t.Fatalf("unexpected decoded position: %+v", position)
	}
}

func TestAPRSManagerLocalKISSUpdatesTrackedResponder(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverError := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverError <- err
			return
		}
		defer conn.Close()
		frame := testAX25UIFrame(
			"N0CALL-7",
			"APRS",
			nil,
			"!3503.50N/08640.25W>Local receiver",
		)
		packet := append([]byte{kissFEND, 0x00}, frame...)
		packet = append(packet, kissFEND)
		if _, err := conn.Write(packet); err != nil {
			serverError <- err
			return
		}
		<-time.After(2 * time.Second)
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
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.Mode = "local"
	settings.APRS.Local.Decoder = "kiss_tcp"
	settings.APRS.Local.KISSAddress = listener.Addr().String()
	settings.APRS.Local.ShowAll = true
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

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := store.Snapshot()
		if len(snapshot.Tracks) == 1 {
			if snapshot.Tracks[0].Source != "local_rf" ||
				snapshot.Responders[0].PositionSource != "local_rf" {
				t.Fatalf("local source was not retained: %+v %+v", snapshot.Tracks[0], snapshot.Responders[0])
			}
			status := manager.Status().Local
			if status.PacketsReceived != 1 || status.PositionPacketsDecoded != 1 ||
				status.PositionsDisplayed != 1 || status.PositionsUpdated != 1 || status.State != "connected" {
				t.Fatalf("unexpected local APRS status: %+v", status)
			}
			stations := manager.Activity()
			if len(stations) != 1 || stations[0].Callsign != "N0CALL-7" || stations[0].Source != "local_rf" {
				t.Fatalf("locally decoded station was not displayed: %+v", stations)
			}
			return
		}
		select {
		case serverErr := <-serverError:
			t.Fatal(serverErr)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("local KISS position was not saved")
}

func TestInternetOnlyModeDoesNotEnableLocalReceiver(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.Mode = "internet"
	settings.APRS.LoginCallsign = "N0CALL"
	if err := store.SaveAPRSPasscode("12345"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	if manager.localConfig().enabled {
		t.Fatal("Internet-only mode enabled the local receiver")
	}
}

func TestLocalDecodedPositionIsDisplayedBeforeCrossSourceDedup(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Snapshot().Settings
	settings.APRS.Enabled = true
	settings.APRS.Mode = "hybrid"
	settings.APRS.LoginCallsign = "N0CALL"
	settings.APRS.Local.Decoder = "kiss_tcp"
	settings.APRS.Local.KISSAddress = "127.0.0.1:8001"
	settings.APRS.Local.ShowAll = true
	if err = store.SaveAPRSPasscode("12345"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	packetAt := time.Now().UTC()
	position := APRSPosition{
		Callsign: "N0LOCAL-7", Latitude: 35.0583, Longitude: -86.6708,
		Raw: "N0LOCAL-7>APRS:!3503.50N/08640.25W>Local receiver", ReceivedAt: packetAt,
	}

	// Simulate APRS-IS winning the Hybrid race before the same radio-heard copy.
	if manager.duplicate(position.Raw, packetAt) {
		t.Fatal("first packet was unexpectedly treated as a duplicate")
	}
	manager.processLocalPosition(manager.localConfig(), position, packetAt)

	stations := manager.Activity()
	if len(stations) != 1 || stations[0].Callsign != "N0LOCAL-7" || stations[0].Source != "local_rf" {
		t.Fatalf("radio-heard position was not displayed after cross-source dedup: %+v", stations)
	}
	status := manager.Status().Local
	if status.PositionsDisplayed != 1 {
		t.Fatalf("local displayed positions = %d, want 1", status.PositionsDisplayed)
	}
}

func TestGeneratedDireWolfConfigIsReceiveOnlyOnRadio(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	path, cleanup, err := manager.writeDireWolfConfig(localAPRSRuntimeConfig{
		audioDevice:       "USB Audio CODEC",
		audioOutputDevice: "Mac mini Speakers",
		igateEnabled:      true,
		server:            "rotate.aprs2.net:14580",
		login:             "N0CALL",
		passcode:          "12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config := string(data)
	for _, required := range []string{
		`ADEVICE "USB Audio CODEC" "Mac mini Speakers"`,
		"KISSPORT 8788 0",
		"IGSERVER rotate.aprs2.net:14580",
		"IGLOGIN N0CALL 12345",
	} {
		if !strings.Contains(config, required) {
			t.Fatalf("Dire Wolf config is missing %q:\n%s", required, config)
		}
	}
	for _, forbidden := range []string{"IGTXVIA", "DIGIPEAT", "PBEACON", "OBEACON"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("Dire Wolf config unexpectedly enables RF transmission with %q", forbidden)
		}
	}
}

func TestParseDireWolfAudioDevices(t *testing.T) {
	output := `Dire Wolf Release 1.8.1
Number of devices = 3
--------------------------------------- device #0
[ Default Input ]
Name        = "USB Audio CODEC"
Host API    = Core Audio
Max inputs  = 2
Max outputs = 0
--------------------------------------- device #1
[ Default Output ]
Name        = "Mac mini Speakers"
Host API    = Core Audio
Max inputs  = 0
Max outputs = 2
--------------------------------------- device #2
Name        = "Radio Interface"
Host API    = Core Audio
Max inputs  = 1
Max outputs = 2
`
	version, devices := parseDireWolfAudioDevices(output)
	if version != "Dire Wolf Release 1.8.1" {
		t.Fatalf("version = %q", version)
	}
	if len(devices) != 3 {
		t.Fatalf("device count = %d, want 3: %+v", len(devices), devices)
	}
	if devices[0].Name != "USB Audio CODEC" || !devices[0].DefaultInput ||
		devices[0].MaxInputChannels != 2 || devices[0].MaxOutputChannels != 0 {
		t.Fatalf("unexpected input device: %+v", devices[0])
	}
	if devices[1].Name != "Mac mini Speakers" || !devices[1].DefaultOutput {
		t.Fatalf("unexpected output device: %+v", devices[1])
	}
	if devices[2].HostAPI != "Core Audio" || devices[2].MaxInputChannels != 1 ||
		devices[2].MaxOutputChannels != 2 {
		t.Fatalf("unexpected duplex device: %+v", devices[2])
	}
}

// Reuse the native test executable so discovery exercises a real child process
// on Windows as well as Unix, without requiring a shell or installed decoder.
func TestMain(m *testing.M) {
	if os.Getenv("TICKETS_TEST_AUDIO_DECODER") == "1" {
		fmt.Print(`Dire Wolf Release 1.8.1
Number of devices = 2
--------------------------------------- device #0
[ Default Input ]
Name = "USB Audio CODEC"
Host API = Core Audio
Max inputs = 2
Max outputs = 0
--------------------------------------- device #1
[ Default Output ]
Name = "Mac mini Speakers"
Host API = Core Audio
Max inputs = 0
Max outputs = 2
`)
		os.Exit(1) // Dire Wolf can report devices while rejecting the scan device.
	}
	os.Exit(m.Run())
}

func TestDiscoverLocalAudioDevicesWithBundledDecoder(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TICKETS_TEST_AUDIO_DECODER", "1")
	t.Setenv("TICKETS_LOCAL_DIREWOLF", executable)
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	discovery, err := manager.DiscoverLocalAudioDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !discovery.DecoderAvailable || discovery.DecoderVersion != "Dire Wolf Release 1.8.1" ||
		len(discovery.Devices) != 2 {
		t.Fatalf("unexpected discovery: %+v", discovery)
	}
	if discovery.Devices[0].Name != "USB Audio CODEC" || discovery.Devices[1].Name != "Mac mini Speakers" {
		t.Fatalf("unexpected devices: %+v", discovery.Devices)
	}
}

func TestDireWolfMonitorReportsActivityAndRedactsPasscode(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewAPRSManager(store, log.New(io.Discard, "", 0))
	monitor := newDireWolfMonitor(manager, "12345")
	if _, err := monitor.Write([]byte("Dire Wolf Release 1.8.1\nN0CALL audio level = 42 [NONE]\nIGLOGIN N0CALL 12345\n")); err != nil {
		t.Fatal(err)
	}
	status := manager.Status().Local
	if status.DecoderVersion != "Dire Wolf Release 1.8.1" {
		t.Fatalf("decoder version = %q", status.DecoderVersion)
	}
	if status.AudioLevel == nil || *status.AudioLevel != 42 {
		t.Fatalf("audio level = %v", status.AudioLevel)
	}
	if len(status.RecentMessages) != 3 {
		t.Fatalf("recent message count = %d", len(status.RecentMessages))
	}
	for _, message := range status.RecentMessages {
		if strings.Contains(message, "12345") {
			t.Fatalf("passcode leaked in status message: %q", message)
		}
	}
}

type testAX25Path struct {
	callsign string
	repeated bool
}

func testAX25UIFrame(source, destination string, path []testAX25Path, information string) []byte {
	addresses := []testAX25Path{{callsign: destination}, {callsign: source}}
	addresses = append(addresses, path...)
	frame := make([]byte, 0, len(addresses)*7+2+len(information))
	for index, address := range addresses {
		frame = append(frame, encodeTestAX25Address(address.callsign, address.repeated, index == len(addresses)-1)...)
	}
	frame = append(frame, 0x03, 0xf0)
	frame = append(frame, information...)
	return frame
}

func encodeTestAX25Address(value string, repeated, last bool) []byte {
	call := value
	ssid := 0
	for index := 0; index < len(value); index++ {
		if value[index] == '-' {
			call = value[:index]
			for _, digit := range value[index+1:] {
				ssid = ssid*10 + int(digit-'0')
			}
			break
		}
	}
	encoded := make([]byte, 7)
	for index := 0; index < 6; index++ {
		character := byte(' ')
		if index < len(call) {
			character = call[index]
		}
		encoded[index] = character << 1
	}
	encoded[6] = 0x60 | byte(ssid<<1)
	if repeated {
		encoded[6] |= 0x80
	}
	if last {
		encoded[6] |= 0x01
	}
	return encoded
}
