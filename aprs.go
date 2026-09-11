package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type aprsDialer func(context.Context, string, string) (net.Conn, error)

type APRSManager struct {
	store         *Store
	logger        *log.Logger
	dial          aprsDialer
	statusMu      sync.RWMutex
	status        APRSStatus
	localStatusMu sync.RWMutex
	localStatus   APRSLocalStatus
	generation    atomic.Uint64
	cancel        context.CancelFunc
	done          chan struct{}
	seenMu        sync.Mutex
	seen          map[[32]byte]time.Time
	activityMu    sync.RWMutex
	activity      map[string]APRSStation
}

type aprsRuntimeConfig struct {
	enabled        bool
	mode           string
	server         string
	login          string
	passcode       string
	filter         string
	callsign       map[string]struct{}
	family         map[string]struct{}
	areaEnabled    bool
	areaLatitude   float64
	areaLongitude  float64
	areaRadiusKM   float64
	validationOnly bool
	key            string
}

type localAPRSRuntimeConfig struct {
	enabled           bool
	mode              string
	decoder           string
	kissAddress       string
	audioDevice       string
	audioOutputDevice string
	showAll           bool
	igateEnabled      bool
	loginVerified     bool
	server            string
	login             string
	passcode          string
	callsign          map[string]struct{}
	family            map[string]struct{}
	areaEnabled       bool
	areaLatitude      float64
	areaLongitude     float64
	areaRadiusKM      float64
	key               string
}

func NewAPRSManager(store *Store, logger *log.Logger) *APRSManager {
	dialer := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
	return &APRSManager{
		store:       store,
		logger:      logger,
		dial:        dialer.DialContext,
		status:      APRSStatus{Mode: "internet", State: "disabled", Message: "APRS tracking is off"},
		localStatus: APRSLocalStatus{State: "disabled", Message: "Local RF reception is off"},
		done:        make(chan struct{}),
		seen:        make(map[[32]byte]time.Time),
		activity:    make(map[string]APRSStation),
	}
}

func (manager *APRSManager) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	manager.cancel = cancel
	go func() {
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			manager.run(ctx)
		}()
		go func() {
			defer wait.Done()
			manager.runLocal(ctx)
		}()
		wait.Wait()
		close(manager.done)
	}()
}

func (manager *APRSManager) Stop() {
	if manager.cancel == nil {
		return
	}
	manager.cancel()
	<-manager.done
}

func (manager *APRSManager) Notify() {
	manager.generation.Add(1)
}

func (manager *APRSManager) Status() APRSStatus {
	manager.statusMu.RLock()
	status := manager.status
	manager.statusMu.RUnlock()
	manager.localStatusMu.RLock()
	status.Local = manager.localStatus
	status.Local.RecentMessages = slices.Clone(manager.localStatus.RecentMessages)
	manager.localStatusMu.RUnlock()
	snapshot := manager.store.Snapshot()
	settings := snapshot.Settings.APRS
	manager.activityMu.Lock()
	status.Mode = settings.Mode
	status.Local.IGateVerified = settings.Local.IGateEnabled && status.LoginVerified
	if !aprsActivityEnabled(settings, snapshot.Responders) {
		clear(manager.activity)
	} else {
		manager.pruneActivityLocked(time.Now().UTC())
	}
	status.AreaStations = len(manager.activity)
	manager.activityMu.Unlock()
	return status
}

func (manager *APRSManager) Activity() []APRSStation {
	snapshot := manager.store.Snapshot()
	settings := snapshot.Settings.APRS
	manager.activityMu.Lock()
	defer manager.activityMu.Unlock()
	if !aprsActivityEnabled(settings, snapshot.Responders) {
		clear(manager.activity)
		return []APRSStation{}
	}
	manager.pruneActivityLocked(time.Now().UTC())
	stations := make([]APRSStation, 0, len(manager.activity))
	for _, station := range manager.activity {
		stations = append(stations, station)
	}
	slices.SortFunc(stations, func(left, right APRSStation) int {
		return right.LastHeardAt.Compare(left.LastHeardAt)
	})
	return stations
}

func (manager *APRSManager) run(ctx context.Context) {
	backoff := 2 * time.Second
	for {
		if ctx.Err() != nil {
			manager.updateStatus(func(status *APRSStatus) {
				status.State = "stopped"
				status.Message = "APRS receiver stopped"
				status.ConnectedAt = nil
				status.LoginVerified = false
				status.LoginVerifiedAt = nil
			})
			return
		}
		config := manager.config()
		if !config.enabled {
			manager.setWaitingStatus(config)
			if !waitForContext(ctx, 2*time.Second) {
				return
			}
			continue
		}

		generation := manager.generation.Load()
		err := manager.connectAndRead(ctx, config, generation)
		if ctx.Err() != nil {
			continue
		}
		if manager.generation.Load() != generation {
			backoff = 2 * time.Second
			continue
		}
		manager.updateStatus(func(status *APRSStatus) {
			if errors.Is(err, errAPRSLoginRejected) {
				status.State = "authentication"
			} else {
				status.State = "reconnecting"
			}
			status.Message = friendlyAPRSError(err)
			status.ConnectedAt = nil
			status.LoginVerified = false
			status.LoginVerifiedAt = nil
			status.Reconnects++
		})
		manager.logger.Printf("APRS-IS connection: %v", err)
		if !waitForContext(ctx, backoff) {
			return
		}
		if backoff < time.Minute {
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
		}
	}
}

func (manager *APRSManager) config() aprsRuntimeConfig {
	snapshot := manager.store.Snapshot()
	settings := snapshot.Settings.APRS
	passcode := manager.store.APRSPasscode()
	callsigns := make([]string, 0)
	callset := make(map[string]struct{})
	familyset := make(map[string]struct{})
	for _, responder := range snapshot.Responders {
		if responder.APRSEnabled && validTrackedCallsign(responder.Callsign) {
			call := strings.ToUpper(responder.Callsign)
			if _, exists := callset[call]; !exists {
				callset[call] = struct{}{}
				family := aprsCallsignFamily(call)
				if _, familyExists := familyset[family]; !familyExists {
					familyset[family] = struct{}{}
					callsigns = append(callsigns, family+"*")
				}
			}
		}
	}
	slices.Sort(callsigns)
	filterParts := make([]string, 0, 2)
	validationOnly := settings.Mode == "local" && settings.Local.IGateEnabled
	if validationOnly && validAPRSISLogin(settings.LoginCallsign) {
		filterParts = append(filterParts, "b/"+settings.LoginCallsign)
		callset = make(map[string]struct{})
		familyset = make(map[string]struct{})
	} else if len(callsigns) > 0 {
		filterParts = append(filterParts, "b/"+strings.Join(callsigns, "/"))
	}
	if settings.ExtraFilter != "" && !validationOnly {
		filterParts = append(filterParts, settings.ExtraFilter)
	}
	areaRadiusKM := float64(settings.AreaRadiusMiles) * 1.609344
	if settings.AreaEnabled && !validationOnly {
		filterParts = append(filterParts, fmt.Sprintf(
			"r/%.6f/%.6f/%.1f",
			snapshot.Settings.CenterLat,
			snapshot.Settings.CenterLon,
			areaRadiusKM,
		))
	}
	filter := strings.Join(filterParts, " ")
	passcodeHash := sha256.Sum256([]byte(passcode))
	internetMode := settings.Mode == "internet" || settings.Mode == "hybrid" || settings.Local.IGateEnabled
	key := fmt.Sprintf("%t|%s|%s|%s|%s|%x", settings.Enabled, settings.Mode, settings.Server, settings.LoginCallsign, filter, passcodeHash)
	return aprsRuntimeConfig{
		enabled:        settings.Enabled && internetMode && validAPRSISLogin(settings.LoginCallsign) && passcode != "" && (len(callsigns) > 0 || settings.AreaEnabled || validationOnly),
		mode:           settings.Mode,
		server:         settings.Server,
		login:          settings.LoginCallsign,
		passcode:       passcode,
		filter:         filter,
		callsign:       callset,
		family:         familyset,
		areaEnabled:    settings.AreaEnabled && !validationOnly,
		areaLatitude:   snapshot.Settings.CenterLat,
		areaLongitude:  snapshot.Settings.CenterLon,
		areaRadiusKM:   areaRadiusKM,
		validationOnly: validationOnly,
		key:            key,
	}
}

func (manager *APRSManager) setWaitingStatus(config aprsRuntimeConfig) {
	settings := manager.store.Snapshot().Settings.APRS
	manager.updateStatus(func(status *APRSStatus) {
		status.Server = settings.Server
		status.Filter = config.filter
		status.ConnectedAt = nil
		status.LoginVerified = false
		status.LoginVerifiedAt = nil
		switch {
		case !settings.Enabled:
			status.State = "disabled"
			status.Message = "APRS tracking is off"
		case settings.Mode == "local" && !settings.Local.IGateEnabled:
			status.State = "disabled"
			status.Message = "Internet feed is off in Local RF mode"
		case !validAPRSISLogin(settings.LoginCallsign):
			status.State = "configuration"
			status.Message = "Enter a valid APRS-IS login callsign"
		case config.passcode == "":
			status.State = "configuration"
			status.Message = "Enter an APRS-IS passcode"
		case len(config.callsign) == 0 && !settings.AreaEnabled:
			status.State = "waiting"
			status.Message = "Track a responder or enable nearby APRS activity"
		default:
			status.State = "waiting"
			status.Message = "Waiting to connect"
		}
	})
}

func (manager *APRSManager) connectAndRead(ctx context.Context, config aprsRuntimeConfig, generation uint64) error {
	manager.updateStatus(func(status *APRSStatus) {
		status.State = "connecting"
		status.Message = "Connecting to APRS-IS"
		status.Server = config.server
		status.Filter = config.filter
		status.ConnectedAt = nil
		status.LoginVerified = false
		status.LoginVerifiedAt = nil
	})
	conn, err := manager.dial(ctx, "tcp", config.server)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return
			case <-stopWatcher:
				return
			case <-ticker.C:
				if manager.generation.Load() != generation || manager.config().key != config.key {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024), 1024)
	if err := conn.SetReadDeadline(time.Now().Add(12 * time.Second)); err != nil {
		return err
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read server greeting: %w", err)
		}
		return errors.New("APRS-IS server closed before greeting")
	}
	login := fmt.Sprintf("user %s pass %s vers TicketsLocal %s", config.login, config.passcode, version)
	if config.filter != "" {
		login += " filter " + config.filter
	}
	if _, err := fmt.Fprintf(conn, "%s\r\n", login); err != nil {
		return fmt.Errorf("send APRS-IS login: %w", err)
	}
	manager.updateStatus(func(status *APRSStatus) {
		status.State = "authenticating"
		status.Message = "Waiting for APRS-IS login validation"
	})
	if err := validateAPRSISLogin(scanner, conn, config.login); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})

	now := time.Now().UTC()
	manager.updateStatus(func(status *APRSStatus) {
		status.State = "connected"
		if config.validationOnly {
			status.Message = "APRS-IS login verified for the local iGate"
		} else {
			status.Message = "Verified APRS-IS feed is live"
		}
		status.ConnectedAt = &now
		status.LoginVerified = true
		status.LoginVerifiedAt = &now
	})
	manager.logger.Printf("APRS-IS connected to %s with filter %s", config.server, config.filter)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !scanner.Scan() {
			err := scanner.Err()
			if manager.generation.Load() != generation || manager.config().key != config.key {
				return errors.New("APRS configuration changed")
			}
			if err != nil {
				return fmt.Errorf("read APRS-IS feed: %w", err)
			}
			return errors.New("APRS-IS server closed the connection")
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		packetAt := time.Now().UTC()
		manager.updateStatus(func(status *APRSStatus) {
			status.LastPacketAt = &packetAt
			status.PacketsReceived++
		})
		position, err := parseAPRSPacket(line, packetAt)
		if err != nil {
			continue
		}
		manager.updateStatus(func(status *APRSStatus) {
			status.PositionPacketsDecoded++
		})
		if manager.duplicate(position.Raw, packetAt) {
			continue
		}
		if _, tracked := config.callsign[position.Callsign]; tracked {
			_, updated, err := manager.store.RecordAPRSPosition(position, "aprs_is")
			if err != nil {
				manager.logger.Printf("save APRS position for %s: %v", position.Callsign, err)
			} else if updated {
				manager.updateStatus(func(status *APRSStatus) {
					status.LastPositionAt = &packetAt
					status.PositionsUpdated++
				})
			}
		}
		_, exactTracked := config.callsign[position.Callsign]
		_, trackedFamily := config.family[aprsCallsignFamily(position.Callsign)]
		siblingSSID := trackedFamily && !exactTracked
		if siblingSSID || (config.areaEnabled && distanceKilometers(
			config.areaLatitude,
			config.areaLongitude,
			position.Latitude,
			position.Longitude,
		) <= config.areaRadiusKM) {
			manager.recordActivity(position, "aprs_is")
			manager.updateStatus(func(status *APRSStatus) {
				status.LastAreaPositionAt = &packetAt
				status.AreaPositionsReceived++
			})
		}
	}
}

func (manager *APRSManager) runLocal(ctx context.Context) {
	backoff := 2 * time.Second
	for {
		if ctx.Err() != nil {
			manager.updateLocalStatus(func(status *APRSLocalStatus) {
				status.State = "stopped"
				status.Message = "Local RF receiver stopped"
				status.ConnectedAt = nil
				status.DecoderRunning = false
			})
			return
		}
		config := manager.localConfig()
		if !config.enabled {
			manager.setLocalWaitingStatus(config)
			if !waitForContext(ctx, 2*time.Second) {
				return
			}
			continue
		}

		generation := manager.generation.Load()
		err := manager.connectAndReadLocal(ctx, config, generation)
		if ctx.Err() != nil {
			continue
		}
		if manager.generation.Load() != generation {
			backoff = 2 * time.Second
			continue
		}
		manager.updateLocalStatus(func(status *APRSLocalStatus) {
			status.State = localAPRSErrorState(err)
			status.Message = friendlyLocalAPRSError(err)
			status.ConnectedAt = nil
			status.DecoderRunning = false
			status.Reconnects++
		})
		manager.logger.Printf("local APRS connection: %v", err)
		if !waitForContext(ctx, backoff) {
			return
		}
		if backoff < time.Minute {
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
		}
	}
}

func (manager *APRSManager) localConfig() localAPRSRuntimeConfig {
	snapshot := manager.store.Snapshot()
	settings := snapshot.Settings.APRS
	passcode := manager.store.APRSPasscode()
	callset := make(map[string]struct{})
	familyset := make(map[string]struct{})
	for _, responder := range snapshot.Responders {
		if responder.APRSEnabled && validTrackedCallsign(responder.Callsign) {
			call := strings.ToUpper(responder.Callsign)
			callset[call] = struct{}{}
			familyset[aprsCallsignFamily(call)] = struct{}{}
		}
	}
	localMode := settings.Mode == "local" || settings.Mode == "hybrid"
	kissAddress := settings.Local.KISSAddress
	if settings.Local.Decoder == "bundled" {
		kissAddress = "127.0.0.1:8788"
	}
	areaRadiusKM := float64(settings.AreaRadiusMiles) * 1.609344
	passcodeHash := sha256.Sum256([]byte(passcode))
	manager.statusMu.RLock()
	loginVerified := manager.status.LoginVerified
	manager.statusMu.RUnlock()
	key := fmt.Sprintf(
		"%t|%s|%s|%s|%s|%s|%t|%t|%t|%s|%s|%x",
		settings.Enabled,
		settings.Mode,
		settings.Local.Decoder,
		kissAddress,
		settings.Local.AudioDevice,
		settings.Local.AudioOutputDevice,
		settings.Local.ShowAll,
		settings.Local.IGateEnabled,
		loginVerified,
		settings.Server,
		settings.LoginCallsign,
		passcodeHash,
	)
	return localAPRSRuntimeConfig{
		enabled:           settings.Enabled && localMode && (len(callset) > 0 || settings.Local.ShowAll) && (!settings.Local.IGateEnabled || loginVerified),
		mode:              settings.Mode,
		decoder:           settings.Local.Decoder,
		kissAddress:       kissAddress,
		audioDevice:       settings.Local.AudioDevice,
		audioOutputDevice: settings.Local.AudioOutputDevice,
		showAll:           settings.Local.ShowAll,
		igateEnabled:      settings.Local.IGateEnabled,
		loginVerified:     loginVerified,
		server:            settings.Server,
		login:             settings.LoginCallsign,
		passcode:          passcode,
		callsign:          callset,
		family:            familyset,
		areaEnabled:       settings.AreaEnabled,
		areaLatitude:      snapshot.Settings.CenterLat,
		areaLongitude:     snapshot.Settings.CenterLon,
		areaRadiusKM:      areaRadiusKM,
		key:               key,
	}
}

func (manager *APRSManager) setLocalWaitingStatus(config localAPRSRuntimeConfig) {
	settings := manager.store.Snapshot().Settings.APRS
	manager.updateLocalStatus(func(status *APRSLocalStatus) {
		status.Decoder = settings.Local.Decoder
		status.KISSAddress = config.kissAddress
		status.IGateEnabled = settings.Local.IGateEnabled
		status.ConnectedAt = nil
		status.DecoderRunning = false
		status.AudioLevel = nil
		if settings.Local.Decoder == "bundled" {
			status.DecoderArchitecture = runtime.GOARCH
			path, err := bundledDireWolfPath()
			status.DecoderAvailable = err == nil
			if err == nil && status.DecoderVersion == "" {
				status.DecoderVersion = filepath.Base(path)
			}
		} else {
			status.DecoderAvailable = true
			status.DecoderArchitecture = ""
			status.DecoderVersion = ""
		}
		switch {
		case !settings.Enabled:
			status.State = "disabled"
			status.Message = "Local RF reception is off"
		case settings.Mode == "internet":
			status.State = "disabled"
			status.Message = "Local RF is off in Internet-only mode"
		case settings.Local.IGateEnabled && !config.loginVerified:
			status.State = "waiting"
			status.Message = "Waiting for APRS-IS login validation before starting the local iGate"
		case len(config.callsign) == 0 && !settings.Local.ShowAll:
			status.State = "waiting"
			status.Message = "Track a responder or enable all locally heard positions"
		default:
			status.State = "waiting"
			status.Message = "Waiting to start local RF reception"
		}
	})
}

func (manager *APRSManager) connectAndReadLocal(ctx context.Context, config localAPRSRuntimeConfig, generation uint64) error {
	localContext, cancel := context.WithCancel(ctx)
	defer cancel()

	var decoderDone <-chan error
	if config.decoder == "bundled" {
		path, err := bundledDireWolfPath()
		if err != nil {
			return err
		}
		configPath, cleanup, err := manager.writeDireWolfConfig(config)
		if err != nil {
			return err
		}
		defer cleanup()
		manager.updateLocalStatus(func(status *APRSLocalStatus) {
			status.RecentMessages = []string{}
			status.AudioLevel = nil
			status.DecoderAvailable = true
			status.DecoderArchitecture = runtime.GOARCH
		})
		command := exec.CommandContext(localContext, path, "-t", "0", "-q", "d", "-a", "10", "-c", configPath)
		monitor := newDireWolfMonitor(manager, config.passcode)
		command.Stdout = monitor
		command.Stderr = monitor
		if err := command.Start(); err != nil {
			return fmt.Errorf("start bundled Dire Wolf: %w", err)
		}
		done := make(chan error, 1)
		decoderDone = done
		go func() {
			done <- command.Wait()
			close(done)
		}()
		manager.updateLocalStatus(func(status *APRSLocalStatus) {
			status.State = "starting"
			status.Message = "Starting the bundled Dire Wolf decoder"
			status.Decoder = config.decoder
			status.KISSAddress = config.kissAddress
			status.DecoderRunning = true
			status.IGateEnabled = config.igateEnabled
		})
	}

	conn, err := manager.connectLocalKISS(localContext, config, decoderDone)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-localContext.Done():
				_ = conn.Close()
				return
			case <-stopWatcher:
				return
			case <-ticker.C:
				if manager.generation.Load() != generation || manager.localConfig().key != config.key {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	now := time.Now().UTC()
	manager.updateLocalStatus(func(status *APRSLocalStatus) {
		status.State = "connected"
		if config.decoder == "bundled" {
			status.Message = "Local radio audio decoder is live"
			status.DecoderRunning = true
		} else {
			status.Message = "Local KISS TNC feed is live"
		}
		status.Decoder = config.decoder
		status.KISSAddress = config.kissAddress
		status.ConnectedAt = &now
		status.IGateEnabled = config.igateEnabled
	})

	err = readKISSFrames(conn, func(frame []byte) {
		packetAt := time.Now().UTC()
		manager.updateLocalStatus(func(status *APRSLocalStatus) {
			status.LastPacketAt = &packetAt
			status.PacketsReceived++
		})
		line, err := ax25UIToTNC2(frame)
		if err != nil {
			return
		}
		position, err := parseAPRSPacket(line, packetAt)
		if err != nil {
			return
		}
		manager.updateLocalStatus(func(status *APRSLocalStatus) {
			status.PositionPacketsDecoded++
		})
		manager.processLocalPosition(config, position, packetAt)
	})
	if manager.generation.Load() != generation || manager.localConfig().key != config.key {
		return errors.New("local APRS configuration changed")
	}
	return err
}

func (manager *APRSManager) processLocalPosition(config localAPRSRuntimeConfig, position APRSPosition, packetAt time.Time) {
	inConfiguredArea := config.areaEnabled && distanceKilometers(
		config.areaLatitude,
		config.areaLongitude,
		position.Latitude,
		position.Longitude,
	) <= config.areaRadiusKM
	_, exactTracked := config.callsign[position.Callsign]
	_, trackedFamily := config.family[aprsCallsignFamily(position.Callsign)]
	if config.showAll || inConfiguredArea || (trackedFamily && !exactTracked) {
		// A packet heard directly over RF must remain visible even when Hybrid mode
		// already received the same packet from APRS-IS. Cross-source deduplication
		// prevents duplicate responder tracks, not local station awareness.
		manager.recordActivity(position, "local_rf")
		manager.updateLocalStatus(func(status *APRSLocalStatus) {
			status.PositionsDisplayed++
		})
	}
	if manager.duplicate(position.Raw, packetAt) {
		return
	}
	if _, tracked := config.callsign[position.Callsign]; !tracked {
		return
	}
	_, updated, err := manager.store.RecordAPRSPosition(position, "local_rf")
	if err != nil {
		manager.logger.Printf("save local APRS position for %s: %v", position.Callsign, err)
	} else if updated {
		manager.updateLocalStatus(func(status *APRSLocalStatus) {
			status.LastPositionAt = &packetAt
			status.PositionsUpdated++
		})
	}
}

func (manager *APRSManager) connectLocalKISS(ctx context.Context, config localAPRSRuntimeConfig, decoderDone <-chan error) (net.Conn, error) {
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := manager.dial(ctx, "tcp", config.kissAddress)
		if err == nil {
			return conn, nil
		}
		if decoderDone == nil {
			return nil, fmt.Errorf("connect to KISS TNC at %s: %w", config.kissAddress, err)
		}
		select {
		case decoderErr := <-decoderDone:
			if decoderErr == nil {
				decoderErr = errors.New("decoder exited")
			}
			return nil, fmt.Errorf("bundled Dire Wolf stopped before KISS was ready: %w", decoderErr)
		default:
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("bundled Dire Wolf did not open KISS port %s", config.kissAddress)
		}
		if !waitForContext(ctx, 300*time.Millisecond) {
			return nil, ctx.Err()
		}
	}
}

func (manager *APRSManager) writeDireWolfConfig(config localAPRSRuntimeConfig) (string, func(), error) {
	runtimeDir, err := os.MkdirTemp("", "tickets-local-direwolf-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create Dire Wolf runtime directory: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(runtimeDir)
	}
	if strings.TrimSpace(config.audioDevice) == "" {
		cleanup()
		return "", func() {}, errors.New("select a radio audio input in APRS settings")
	}
	if strings.TrimSpace(config.audioOutputDevice) == "" {
		cleanup()
		return "", func() {}, errors.New("select an audio output in APRS settings")
	}
	login := config.login
	if !validAPRSISLogin(login) {
		login = "N0CALL"
	}
	lines := []string{
		"# Generated by Tickets Local. Receive-only radio configuration.",
		"ACHANNELS 1",
		"CHANNEL 0",
		"MYCALL " + login,
		"MODEM 1200",
		"AGWPORT 0",
		"KISSPORT 0",
		"KISSPORT 8788 0",
	}
	lines = append([]string{
		"ADEVICE " + direWolfQuoted(config.audioDevice) + " " + direWolfQuoted(config.audioOutputDevice),
	}, lines...)
	if config.igateEnabled {
		if !validAPRSISLogin(config.login) || config.passcode == "" {
			cleanup()
			return "", func() {}, errors.New("local iGate requires a valid APRS-IS callsign and passcode")
		}
		lines = append(lines,
			"IGSERVER "+config.server,
			"IGLOGIN "+config.login+" "+config.passcode,
		)
	}
	configPath := filepath.Join(runtimeDir, "direwolf.conf")
	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write Dire Wolf configuration: %w", err)
	}
	return configPath, cleanup, nil
}

func (manager *APRSManager) DiscoverLocalAudioDevices(ctx context.Context) (APRSAudioDeviceDiscovery, error) {
	discovery := APRSAudioDeviceDiscovery{
		DecoderArchitecture: runtime.GOARCH,
		Devices:             []APRSAudioDevice{},
	}
	path, err := bundledDireWolfPath()
	if err != nil {
		discovery.Message = friendlyLocalAPRSError(err)
		return discovery, err
	}
	discovery.DecoderAvailable = true

	runtimeDir, err := os.MkdirTemp("", "tickets-local-direwolf-scan-")
	if err != nil {
		return discovery, fmt.Errorf("create audio scan directory: %w", err)
	}
	defer os.RemoveAll(runtimeDir)
	configPath := filepath.Join(runtimeDir, "direwolf-scan.conf")
	scanConfig := strings.Join([]string{
		"# Tickets Local audio-device discovery. No radio or network transmission.",
		`ADEVICE "__TICKETS_LOCAL_DEVICE_SCAN__" "__TICKETS_LOCAL_DEVICE_SCAN__"`,
		"ACHANNELS 1",
		"CHANNEL 0",
		"MYCALL N0CALL",
		"MODEM 1200",
		"AGWPORT 0",
		"KISSPORT 0",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(scanConfig), 0o600); err != nil {
		return discovery, fmt.Errorf("write audio scan configuration: %w", err)
	}

	scanContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(scanContext, path, "-t", "0", "-c", configPath)
	output := &cappedOutput{max: 1 << 20}
	command.Stdout = output
	command.Stderr = output
	runErr := command.Run()
	if scanContext.Err() != nil {
		return discovery, errors.New("audio-device scan timed out")
	}
	discovery.DecoderVersion, discovery.Devices = parseDireWolfAudioDevices(output.String())
	if len(discovery.Devices) == 0 {
		if runErr != nil {
			return discovery, fmt.Errorf("Dire Wolf did not report any audio devices: %w", runErr)
		}
		return discovery, errors.New("Dire Wolf did not report any audio devices")
	}
	if discovery.DecoderVersion == "" {
		discovery.DecoderVersion = filepath.Base(path)
	}
	discovery.Message = fmt.Sprintf("Found %d audio devices", len(discovery.Devices))
	manager.updateLocalStatus(func(status *APRSLocalStatus) {
		status.DecoderAvailable = true
		status.DecoderVersion = discovery.DecoderVersion
		status.DecoderArchitecture = discovery.DecoderArchitecture
	})
	return discovery, nil
}

type cappedOutput struct {
	bytes.Buffer
	max int
}

func (output *cappedOutput) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := output.max - output.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = output.Buffer.Write(data)
	}
	return originalLength, nil
}

func parseDireWolfAudioDevices(output string) (string, []APRSAudioDevice) {
	var (
		version string
		devices []APRSAudioDevice
		current *APRSAudioDevice
	)
	flush := func() {
		if current != nil && current.Name != "" {
			devices = append(devices, *current)
		}
		current = nil
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		if version == "" && strings.HasPrefix(lower, "dire wolf ") {
			version = line
		}
		if strings.Contains(lower, "device #") {
			flush()
			current = &APRSAudioDevice{}
			continue
		}
		if current == nil {
			continue
		}
		if strings.Contains(lower, "default input") {
			current.DefaultInput = true
		}
		if strings.Contains(lower, "default output") {
			current.DefaultOutput = true
		}
		switch {
		case strings.HasPrefix(lower, "name") && strings.Contains(line, "="):
			current.Name = strings.Trim(strings.TrimSpace(strings.SplitN(line, "=", 2)[1]), `"`)
		case strings.HasPrefix(lower, "host api") && strings.Contains(line, "="):
			current.HostAPI = strings.Trim(strings.TrimSpace(strings.SplitN(line, "=", 2)[1]), `"`)
		case strings.HasPrefix(lower, "max inputs"):
			current.MaxInputChannels = integerAfterEquals(line)
		case strings.HasPrefix(lower, "max outputs"):
			current.MaxOutputChannels = integerAfterEquals(line)
		}
	}
	flush()
	return version, devices
}

func integerAfterEquals(line string) int {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return 0
	}
	value, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return value
}

type direWolfMonitor struct {
	manager  *APRSManager
	passcode string
	mu       sync.Mutex
	pending  []byte
}

func newDireWolfMonitor(manager *APRSManager, passcode string) *direWolfMonitor {
	return &direWolfMonitor{manager: manager, passcode: passcode}
}

func (monitor *direWolfMonitor) Write(data []byte) (int, error) {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	monitor.pending = append(monitor.pending, data...)
	for {
		index := bytes.IndexByte(monitor.pending, '\n')
		if index < 0 {
			if len(monitor.pending) > 8192 {
				monitor.pending = monitor.pending[len(monitor.pending)-8192:]
			}
			break
		}
		line := string(monitor.pending[:index])
		monitor.pending = monitor.pending[index+1:]
		monitor.capture(line)
	}
	return len(data), nil
}

func (monitor *direWolfMonitor) capture(line string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.Trim(line, "-") == "" {
		return
	}
	if monitor.passcode != "" {
		line = strings.ReplaceAll(line, monitor.passcode, "•••••")
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	monitor.manager.logger.Printf("[direwolf] %s", line)
	monitor.manager.updateLocalStatus(func(status *APRSLocalStatus) {
		if strings.HasPrefix(strings.ToLower(line), "dire wolf ") {
			status.DecoderVersion = line
		}
		if level, ok := direWolfAudioLevel(line); ok {
			status.AudioLevel = &level
		}
		status.RecentMessages = append(status.RecentMessages, line)
		if len(status.RecentMessages) > 24 {
			status.RecentMessages = slices.Clone(status.RecentMessages[len(status.RecentMessages)-24:])
		}
	})
}

func direWolfAudioLevel(line string) (int, bool) {
	lower := strings.ToLower(line)
	marker := "audio level ="
	index := strings.Index(lower, marker)
	if index < 0 {
		return 0, false
	}
	value := strings.TrimSpace(line[index+len(marker):])
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	level, err := strconv.Atoi(value[:end])
	return level, err == nil
}

func bundledDireWolfPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("TICKETS_LOCAL_DIREWOLF")); override != "" {
		if info, err := os.Stat(override); err == nil && !info.IsDir() {
			return override, nil
		}
	}
	executable, err := os.Executable()
	if err == nil {
		executableDir := filepath.Dir(executable)
		name := "direwolf"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidates := []string{
			filepath.Join(executableDir, "..", "Resources", "direwolf", runtime.GOARCH, name),
			filepath.Join(executableDir, "..", "Resources", "direwolf", name),
			filepath.Join(executableDir, "direwolf", name),
			filepath.Join(executableDir, name),
		}
		for _, candidate := range candidates {
			candidate = filepath.Clean(candidate)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	if path, lookupErr := exec.LookPath("direwolf"); lookupErr == nil {
		return path, nil
	}
	return "", errors.New("bundled Dire Wolf decoder is unavailable in this build")
}

func direWolfQuoted(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func (manager *APRSManager) recordActivity(position APRSPosition, source string) {
	manager.activityMu.Lock()
	defer manager.activityMu.Unlock()
	// The SSID is part of the APRS station identity. Never collapse CALL-8,
	// CALL-9, or other suffix-bearing stations into their base callsign.
	stationCallsign := strings.ToUpper(strings.TrimSpace(position.Callsign))
	station := manager.activity[stationCallsign]
	station.Callsign = stationCallsign
	station.Latitude = position.Latitude
	station.Longitude = position.Longitude
	station.SpeedKnots = position.SpeedKnots
	station.CourseDegrees = position.CourseDegrees
	station.AltitudeFeet = position.AltitudeFeet
	station.Symbol = position.Symbol
	station.Comment = abbreviate(strings.TrimSpace(position.Comment), 500)
	station.Source = source
	station.LastHeardAt = position.ReceivedAt.UTC()
	station.PositionPackets++
	if position.Weather != nil {
		weather := *position.Weather
		station.Weather = &weather
	}
	manager.activity[stationCallsign] = station
	manager.pruneActivityLocked(position.ReceivedAt.UTC())
	if len(manager.activity) <= 500 {
		return
	}
	var oldestCallsign string
	var oldestTime time.Time
	for callsign, item := range manager.activity {
		if oldestCallsign == "" || item.LastHeardAt.Before(oldestTime) {
			oldestCallsign = callsign
			oldestTime = item.LastHeardAt
		}
	}
	delete(manager.activity, oldestCallsign)
}

var errAPRSLoginRejected = errors.New("APRS-IS login rejected")

func validateAPRSISLogin(scanner *bufio.Scanner, conn net.Conn, loginCallsign string) error {
	if err := conn.SetReadDeadline(time.Now().Add(12 * time.Second)); err != nil {
		return err
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "#" || !strings.EqualFold(fields[1], "logresp") {
			continue
		}
		if !strings.EqualFold(fields[2], loginCallsign) {
			return fmt.Errorf("%w: server replied for a different callsign", errAPRSLoginRejected)
		}
		verification := strings.TrimSuffix(strings.ToLower(fields[3]), ",")
		if verification != "verified" {
			return fmt.Errorf("%w: callsign or passcode was not verified", errAPRSLoginRejected)
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read APRS-IS login response: %w", err)
	}
	return fmt.Errorf("%w: server closed before validation", errAPRSLoginRejected)
}

func (manager *APRSManager) pruneActivityLocked(now time.Time) {
	cutoff := now.Add(-2 * time.Hour)
	for callsign, station := range manager.activity {
		if station.LastHeardAt.Before(cutoff) {
			delete(manager.activity, callsign)
		}
	}
}

func distanceKilometers(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0088
	toRadians := math.Pi / 180
	lat1Radians := lat1 * toRadians
	lat2Radians := lat2 * toRadians
	latitudeDelta := (lat2 - lat1) * toRadians
	longitudeDelta := (lon2 - lon1) * toRadians
	a := math.Sin(latitudeDelta/2)*math.Sin(latitudeDelta/2) +
		math.Cos(lat1Radians)*math.Cos(lat2Radians)*
			math.Sin(longitudeDelta/2)*math.Sin(longitudeDelta/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func (manager *APRSManager) duplicate(raw string, now time.Time) bool {
	hash := sha256.Sum256([]byte(aprsDuplicateKey(raw)))
	manager.seenMu.Lock()
	defer manager.seenMu.Unlock()
	if seenAt, exists := manager.seen[hash]; exists && now.Sub(seenAt) < 2*time.Minute {
		return true
	}
	manager.seen[hash] = now
	if len(manager.seen) > 2048 {
		cutoff := now.Add(-5 * time.Minute)
		for item, seenAt := range manager.seen {
			if seenAt.Before(cutoff) {
				delete(manager.seen, item)
			}
		}
	}
	return false
}

func aprsDuplicateKey(raw string) string {
	colon := strings.IndexByte(raw, ':')
	greater := strings.IndexByte(raw, '>')
	if colon > 0 && greater > 0 && greater < colon {
		return strings.ToUpper(strings.TrimSpace(raw[:greater])) + ":" + raw[colon+1:]
	}
	return raw
}

func aprsCallsignFamily(callsign string) string {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if hyphen := strings.IndexByte(callsign, '-'); hyphen >= 0 {
		return callsign[:hyphen]
	}
	return callsign
}

func (manager *APRSManager) updateStatus(update func(*APRSStatus)) {
	manager.statusMu.Lock()
	defer manager.statusMu.Unlock()
	update(&manager.status)
}

func (manager *APRSManager) updateLocalStatus(update func(*APRSLocalStatus)) {
	manager.localStatusMu.Lock()
	defer manager.localStatusMu.Unlock()
	update(&manager.localStatus)
}

func localActivityEnabled(settings APRSSettings) bool {
	return (settings.Mode == "local" || settings.Mode == "hybrid") && settings.Local.ShowAll
}

func aprsActivityEnabled(settings APRSSettings, responders []Responder) bool {
	if !settings.Enabled {
		return false
	}
	return settings.AreaEnabled || localActivityEnabled(settings) || slices.ContainsFunc(responders, func(responder Responder) bool {
		return responder.APRSEnabled && validTrackedCallsign(responder.Callsign)
	})
}

func waitForContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func friendlyAPRSError(err error) string {
	if err == nil {
		return "APRS-IS connection ended"
	}
	var networkError net.Error
	switch {
	case errors.Is(err, errAPRSLoginRejected):
		return "APRS-IS rejected the callsign or passcode"
	case errors.As(err, &networkError) && networkError.Timeout():
		return "APRS-IS login validation timed out"
	case strings.Contains(err.Error(), "configuration changed"):
		return "Applying APRS settings"
	default:
		return "APRS-IS unavailable; retrying automatically"
	}
}

func friendlyLocalAPRSError(err error) string {
	if err == nil {
		return "Local RF connection ended"
	}
	switch {
	case strings.Contains(err.Error(), "configuration changed"):
		return "Applying local APRS settings"
	case strings.Contains(err.Error(), "unavailable in this build"):
		return "Bundled decoder is not installed; choose Existing KISS TNC or rebuild with Dire Wolf"
	case strings.Contains(err.Error(), "select a radio audio input"):
		return "Select the radio audio input under Local RF setup"
	case strings.Contains(err.Error(), "select an audio output"):
		return "Select an audio output under Local RF setup"
	case strings.Contains(err.Error(), "requires a valid APRS-IS"):
		return "Local iGate needs the APRS-IS callsign and passcode"
	case strings.Contains(err.Error(), "connect to KISS TNC"):
		return "Local KISS TNC is unavailable; retrying automatically"
	default:
		return "Local RF receiver is unavailable; retrying automatically"
	}
}

func localAPRSErrorState(err error) string {
	if err == nil {
		return "reconnecting"
	}
	message := err.Error()
	if strings.Contains(message, "unavailable in this build") ||
		strings.Contains(message, "select a radio audio input") ||
		strings.Contains(message, "select an audio output") ||
		strings.Contains(message, "requires a valid APRS-IS") {
		return "configuration"
	}
	return "reconnecting"
}
