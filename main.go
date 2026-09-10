package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

//go:embed web/*
var webAssets embed.FS

func main() {
	var (
		portFlag    = flag.Int("port", 0, "override the configured TCP port")
		dataDirFlag = flag.String("data-dir", "", "override the application data directory")
		noBrowser   = flag.Bool("no-browser", false, "do not open the browser automatically")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Printf("%s %s\n", productName, version)
		return
	}
	if *portFlag < 0 || *portFlag > 65535 {
		log.Fatal("port must be between 1 and 65535")
	}

	dataDir, err := resolveDataDir(*dataDirFlag)
	if err != nil {
		log.Fatalf("resolve data directory: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatalf("create data directory: %v", err)
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, "tickets-local.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Fatalf("open log file: %v", err)
	}
	defer logFile.Close()
	logger := log.New(logFile, "", log.Ldate|log.Ltime|log.LUTC)

	networkRuntime, err := OpenNetworkRuntime(dataDir, *portFlag, logger)
	if err != nil {
		log.Fatalf("open network configuration: %v", err)
	}
	networkSettings := networkRuntime.Active()

	store, err := OpenStore(dataDir)
	if err != nil {
		log.Fatalf("open local data: %v", err)
	}
	if networkSettings.Mode != networkModeClient {
		if snapshotPath, created, err := store.CreateAutomaticSnapshot(time.Now().UTC()); err != nil {
			logger.Printf("automatic event-log snapshot: %v", err)
		} else if created {
			logger.Printf("automatic event-log snapshot created: %s", snapshotPath)
		}
	}
	appContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if networkSettings.Mode != networkModeClient {
		go runAutomaticSnapshots(appContext, store, logger)
	}
	var aprs *APRSManager
	if networkSettings.Mode != networkModeClient {
		aprs = NewAPRSManager(store, logger)
		aprs.Start(appContext)
		defer aprs.Stop()
	}

	api, err := newAPIServer(store, webAssets, logger)
	if err != nil {
		log.Fatalf("prepare application: %v", err)
	}
	api.aprs = aprs
	api.network = networkRuntime
	api.requestQuit = stop

	listenHost := "127.0.0.1"
	if networkSettings.Mode == networkModeHost {
		listenHost = "0.0.0.0"
	}
	address := net.JoinHostPort(listenHost, strconv.Itoa(networkSettings.ListenPort))
	localURL := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(networkSettings.ListenPort))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		if existingTicketsLocal(localURL) {
			if !*noBrowser {
				_ = openBrowser(localURL)
			}
			return
		}
		log.Fatalf("could not start on %s: %v", address, err)
	}

	server := &http.Server{
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	url := localURL
	logger.Printf("%s %s started at %s; role=%s; data=%s", productName, version, address, networkSettings.Mode, dataDir)
	fmt.Printf("%s is running at %s\nMode: %s\nData: %s\n", productName, url, networkSettings.Mode, dataDir)
	if networkSettings.Mode == networkModeHost {
		for _, hostURL := range LANHostURLs(networkSettings.ListenPort) {
			fmt.Printf("LAN host: %s\n", hostURL)
		}
	}
	fmt.Println("Press Ctrl+C to stop.")

	if !*noBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				logger.Printf("open browser: %v", err)
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-appContext.Done():
		shutdownServer(context.Background(), server)
	case err := <-errCh:
		if err != nil {
			logger.Printf("server error: %v", err)
			log.Fatalf("server error: %v", err)
		}
	}
	logger.Printf("%s stopped", productName)
}

func runAutomaticSnapshots(ctx context.Context, store *Store, logger *log.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if snapshotPath, created, err := store.CreateAutomaticSnapshot(now.UTC()); err != nil {
				logger.Printf("automatic event-log snapshot: %v", err)
			} else if created {
				logger.Printf("automatic event-log snapshot created: %s", snapshotPath)
			}
		}
	}
}

func resolveDataDir(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", productName), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, productName), nil
}

func existingTicketsLocal(baseURL string) bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	response, err := client.Get(baseURL + "/healthz")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK && response.Header.Get("Content-Type") == "application/json; charset=utf-8"
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}
