package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	networkModeStandalone = "standalone"
	networkModeHost       = "host"
	networkModeClient     = "client"
	lanKeyHeader          = "X-Tickets-Local-LAN-Key"
	lanClientHeader       = "X-Tickets-Local-Client"
)

type NetworkSettings struct {
	Mode       string `json:"mode"`
	HostURL    string `json:"host_url"`
	AccessKey  string `json:"access_key"`
	ListenPort int    `json:"listen_port"`
}

type NetworkSettingsInput struct {
	Mode          string `json:"mode"`
	HostURL       string `json:"host_url"`
	AccessKey     string `json:"access_key"`
	ListenPort    int    `json:"listen_port"`
	RegenerateKey bool   `json:"regenerate_key"`
}

type NetworkClient struct {
	Name       string    `json:"name"`
	Address    string    `json:"address"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type NetworkStatus struct {
	Configured       NetworkSettings `json:"configured"`
	ActiveMode       string          `json:"active_mode"`
	ActiveHostURL    string          `json:"active_host_url,omitempty"`
	ActivePort       int             `json:"active_port"`
	RestartRequired  bool            `json:"restart_required"`
	HostURLs         []string        `json:"host_urls"`
	DetectedHostURLs []string        `json:"detected_host_urls"`
	Clients          []NetworkClient `json:"clients"`
	Message          string          `json:"message"`
}

type NetworkTestResult struct {
	Connected   bool   `json:"connected"`
	Message     string `json:"message"`
	HostVersion string `json:"host_version,omitempty"`
}

type NetworkRuntime struct {
	mu         sync.RWMutex
	path       string
	configured NetworkSettings
	active     NetworkSettings
	logger     *log.Logger
	clientName string
	clients    map[string]NetworkClient
}

func OpenNetworkRuntime(dataDir string, portOverride int, logger *log.Logger) (*NetworkRuntime, error) {
	runtime := &NetworkRuntime{
		path:    filepath.Join(dataDir, "network.json"),
		logger:  logger,
		clients: make(map[string]NetworkClient),
	}
	if hostname, err := os.Hostname(); err == nil {
		runtime.clientName = clean(hostname)
	}
	if runtime.clientName == "" {
		runtime.clientName = "Tickets Local client"
	}

	settings := NetworkSettings{
		Mode:       networkModeStandalone,
		ListenPort: 8787,
	}
	file, err := os.Open(runtime.path)
	if err == nil {
		decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&settings)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("read network configuration: %w", decodeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close network configuration: %w", closeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("open network configuration: %w", err)
	}

	normalized, err := normalizeNetworkSettings(NetworkSettingsInput{
		Mode:       settings.Mode,
		HostURL:    settings.HostURL,
		AccessKey:  settings.AccessKey,
		ListenPort: settings.ListenPort,
	}, settings.AccessKey)
	if err != nil {
		return nil, fmt.Errorf("validate network configuration: %w", err)
	}
	if normalized.Mode == networkModeHost && normalized.AccessKey == "" {
		normalized.AccessKey, err = generateLANAccessKey()
		if err != nil {
			return nil, err
		}
		if err := writeNetworkSettings(runtime.path, normalized); err != nil {
			return nil, err
		}
	}
	runtime.configured = normalized
	runtime.active = normalized
	if portOverride != 0 {
		if portOverride < 1 || portOverride > 65535 {
			return nil, errors.New("port must be between 1 and 65,535")
		}
		runtime.active.ListenPort = portOverride
	}
	return runtime, nil
}

func (runtime *NetworkRuntime) Active() NetworkSettings {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.active
}

func (runtime *NetworkRuntime) Status() NetworkStatus {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.pruneClientsLocked(time.Now().UTC())
	clients := make([]NetworkClient, 0, len(runtime.clients))
	for _, client := range runtime.clients {
		clients = append(clients, client)
	}
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].LastSeenAt.After(clients[j].LastSeenAt)
	})
	status := NetworkStatus{
		Configured:       runtime.configured,
		ActiveMode:       runtime.active.Mode,
		ActiveHostURL:    runtime.active.HostURL,
		ActivePort:       runtime.active.ListenPort,
		RestartRequired:  !sameActiveNetworkSettings(runtime.configured, runtime.active),
		HostURLs:         []string{},
		DetectedHostURLs: LANHostURLs(runtime.configured.ListenPort),
		Clients:          clients,
	}
	switch runtime.active.Mode {
	case networkModeHost:
		status.HostURLs = LANHostURLs(runtime.active.ListenPort)
		status.Message = fmt.Sprintf("Hosting shared data for %d recently active client(s)", len(clients))
	case networkModeClient:
		status.Message = "This computer uses the configured Tickets Local host"
	default:
		status.Message = "Standalone mode; data is available only on this computer"
	}
	if status.RestartRequired {
		status.Message = "Network settings saved; quit and reopen Tickets Local to apply them"
	}
	return status
}

func (runtime *NetworkRuntime) Update(input NetworkSettingsInput) (NetworkStatus, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	existingKey := runtime.configured.AccessKey
	if input.RegenerateKey {
		existingKey = ""
		input.AccessKey = ""
	}
	settings, err := normalizeNetworkSettings(input, existingKey)
	if err != nil {
		return NetworkStatus{}, err
	}
	if settings.Mode == networkModeHost && settings.AccessKey == "" {
		settings.AccessKey, err = generateLANAccessKey()
		if err != nil {
			return NetworkStatus{}, err
		}
	}
	if err := writeNetworkSettings(runtime.path, settings); err != nil {
		return NetworkStatus{}, err
	}
	runtime.configured = settings
	if settings.Mode == runtime.active.Mode && settings.ListenPort == runtime.active.ListenPort {
		// The listener and role-owned background services do not need to change.
		// Client destinations/keys and Host keys are safe to use immediately.
		runtime.active = settings
	}
	runtime.pruneClientsLocked(time.Now().UTC())
	status := runtime.statusLocked()
	return status, nil
}

func (runtime *NetworkRuntime) Test(ctx context.Context, input NetworkSettingsInput) (NetworkTestResult, error) {
	runtime.mu.RLock()
	existingKey := runtime.configured.AccessKey
	runtime.mu.RUnlock()
	settings, err := normalizeNetworkSettings(input, existingKey)
	if err != nil {
		return NetworkTestResult{}, err
	}
	if settings.Mode != networkModeClient {
		return NetworkTestResult{
			Connected: true,
			Message:   "No remote host is required for this mode",
		}, nil
	}
	testContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(testContext, http.MethodGet, settings.HostURL+"/healthz", nil)
	if err != nil {
		return NetworkTestResult{}, err
	}
	request.Header.Set(lanKeyHeader, settings.AccessKey)
	request.Header.Set(lanClientHeader, runtime.clientName)
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return NetworkTestResult{}, fmt.Errorf("could not reach the host: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return NetworkTestResult{}, errors.New("the host rejected the LAN access key")
	}
	if response.StatusCode != http.StatusOK {
		return NetworkTestResult{}, fmt.Errorf("host returned HTTP %d", response.StatusCode)
	}
	var health struct {
		Product string `json:"product"`
		Version string `json:"version"`
		Status  string `json:"status"`
		Role    string `json:"role"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&health); err != nil {
		return NetworkTestResult{}, errors.New("the selected address is not a Tickets Local host")
	}
	if health.Product != productName || health.Status != "ok" || health.Role != networkModeHost {
		return NetworkTestResult{}, errors.New("the selected address is not an active Tickets Local host")
	}
	if health.Version != version {
		return NetworkTestResult{}, fmt.Errorf("host version %s does not match this client version %s", health.Version, version)
	}
	return NetworkTestResult{
		Connected:   true,
		Message:     "Connected to the Tickets Local host",
		HostVersion: health.Version,
	}, nil
}

func (runtime *NetworkRuntime) Route(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active := runtime.Active()
		if active.Mode == networkModeClient && shouldProxyToHost(r.URL.Path) {
			runtime.newClientProxy(active).ServeHTTP(w, r)
			return
		}
		if active.Mode == networkModeHost && !loopbackRequest(r) && localHostOnlyPath(r.URL.Path) {
			writeError(w, http.StatusForbidden, "this operation is available only on the Host computer")
			return
		}
		if active.Mode == networkModeHost && protectedLANPath(r.URL.Path) && !loopbackRequest(r) {
			if !runtime.authorizeHostRequest(r, active.AccessKey) {
				w.Header().Set("WWW-Authenticate", `TicketsLocalLAN realm="Tickets Local"`)
				writeError(w, http.StatusUnauthorized, "valid Tickets Local LAN access key required")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (runtime *NetworkRuntime) newClientProxy(settings NetworkSettings) http.Handler {
	target, _ := url.Parse(settings.HostURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		request.Host = target.Host
		request.Header.Set(lanKeyHeader, settings.AccessKey)
		request.Header.Set(lanClientHeader, runtime.clientName)
	}
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		runtime.logger.Printf("LAN host proxy: %v", err)
		writeError(w, http.StatusBadGateway, "Tickets Local host is unavailable; check LAN settings and host power")
	}
	return proxy
}

func (runtime *NetworkRuntime) authorizeHostRequest(r *http.Request, expected string) bool {
	provided := r.Header.Get(lanKeyHeader)
	if provided == "" && (strings.HasPrefix(r.URL.Path, "/api/tracking/owntracks/") || strings.HasPrefix(r.URL.Path, "/api/tracking/opengts/")) {
		_, password, ok := r.BasicAuth()
		if ok {
			provided = password
		}
	}
	if len(provided) != len(expected) ||
		subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return false
	}
	address := remoteHost(r.RemoteAddr)
	name := abbreviate(clean(r.Header.Get(lanClientHeader)), 100)
	if name == "" {
		name = address
	}
	key := address + "|" + name
	now := time.Now().UTC()
	runtime.mu.Lock()
	runtime.clients[key] = NetworkClient{Name: name, Address: address, LastSeenAt: now}
	runtime.pruneClientsLocked(now)
	runtime.mu.Unlock()
	return true
}

func (runtime *NetworkRuntime) statusLocked() NetworkStatus {
	clients := make([]NetworkClient, 0, len(runtime.clients))
	for _, client := range runtime.clients {
		clients = append(clients, client)
	}
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].LastSeenAt.After(clients[j].LastSeenAt)
	})
	status := NetworkStatus{
		Configured:       runtime.configured,
		ActiveMode:       runtime.active.Mode,
		ActiveHostURL:    runtime.active.HostURL,
		ActivePort:       runtime.active.ListenPort,
		RestartRequired:  !sameActiveNetworkSettings(runtime.configured, runtime.active),
		HostURLs:         []string{},
		DetectedHostURLs: LANHostURLs(runtime.configured.ListenPort),
		Clients:          clients,
	}
	switch runtime.active.Mode {
	case networkModeHost:
		status.HostURLs = LANHostURLs(runtime.active.ListenPort)
		status.Message = fmt.Sprintf("Hosting shared data for %d recently active client(s)", len(clients))
	case networkModeClient:
		status.Message = "This computer uses the configured Tickets Local host"
	default:
		status.Message = "Standalone mode; data is available only on this computer"
	}
	if status.RestartRequired {
		status.Message = "Network settings saved; quit and reopen Tickets Local to apply them"
	}
	return status
}

func (runtime *NetworkRuntime) pruneClientsLocked(now time.Time) {
	cutoff := now.Add(-5 * time.Minute)
	for key, client := range runtime.clients {
		if client.LastSeenAt.Before(cutoff) {
			delete(runtime.clients, key)
		}
	}
}

func normalizeNetworkSettings(input NetworkSettingsInput, existingKey string) (NetworkSettings, error) {
	settings := NetworkSettings{
		Mode:       strings.ToLower(clean(input.Mode)),
		HostURL:    strings.TrimRight(clean(input.HostURL), "/"),
		AccessKey:  strings.TrimSpace(input.AccessKey),
		ListenPort: input.ListenPort,
	}
	if settings.Mode == "" {
		settings.Mode = networkModeStandalone
	}
	if !oneOf(settings.Mode, networkModeStandalone, networkModeHost, networkModeClient) {
		return NetworkSettings{}, validationError{"network mode must be Standalone, Host, or Client"}
	}
	if settings.ListenPort == 0 {
		settings.ListenPort = 8787
	}
	if settings.ListenPort < 1024 || settings.ListenPort > 65535 {
		return NetworkSettings{}, validationError{"LAN port must be between 1,024 and 65,535"}
	}
	if settings.AccessKey == "" {
		settings.AccessKey = existingKey
	}
	if settings.AccessKey != "" && !validLANAccessKey(settings.AccessKey) {
		return NetworkSettings{}, validationError{"LAN access key must be 16–128 letters, numbers, dashes, or underscores"}
	}
	if settings.Mode == networkModeClient {
		if settings.HostURL == "" {
			return NetworkSettings{}, validationError{"host address is required in Client mode"}
		}
		hostURL, err := url.Parse(settings.HostURL)
		if err != nil || !oneOf(hostURL.Scheme, "http", "https") || hostURL.Host == "" ||
			hostURL.User != nil || (hostURL.Path != "" && hostURL.Path != "/") ||
			hostURL.RawQuery != "" || hostURL.Fragment != "" {
			return NetworkSettings{}, validationError{"host address must look like http://192.168.1.50:8787"}
		}
		hostURL.Path = ""
		settings.HostURL = strings.TrimRight(hostURL.String(), "/")
		if settings.AccessKey == "" {
			return NetworkSettings{}, validationError{"LAN access key is required in Client mode"}
		}
	}
	return settings, nil
}

func writeNetworkSettings(path string, settings NetworkSettings) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".network-*.json")
	if err != nil {
		return fmt.Errorf("prepare network settings: %w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("protect network settings: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		file.Close()
		return fmt.Errorf("write network settings: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync network settings: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close network settings: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish network settings: %w", err)
	}
	return nil
}

func generateLANAccessKey() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate LAN access key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func validLANAccessKey(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func sameActiveNetworkSettings(a, b NetworkSettings) bool {
	return a.Mode == b.Mode &&
		a.HostURL == b.HostURL &&
		a.AccessKey == b.AccessKey &&
		a.ListenPort == b.ListenPort
}

func shouldProxyToHost(path string) bool {
	if !strings.HasPrefix(path, "/api/") {
		return false
	}
	return path != "/api/shutdown" &&
		path != "/api/network/settings" &&
		path != "/api/network/test" &&
		path != "/api/network/status"
}

func protectedLANPath(path string) bool {
	return path == "/healthz" || (strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/api/mobile/"))
}

func localHostOnlyPath(path string) bool {
	return path == "/api/shutdown" || strings.HasPrefix(path, "/api/network/") || strings.HasPrefix(path, "/api/mobile/admin/")
}

func loopbackRequest(r *http.Request) bool {
	host := remoteHost(r.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func LANHostURLs(port int) []string {
	seen := make(map[string]struct{})
	urls := make([]string, 0)
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return urls
	}
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			ip = ipv4
		}
		host := ip.String()
		if ip.To4() == nil {
			host = "[" + host + "]"
		}
		value := "http://" + host + ":" + strconv.Itoa(port)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		urls = append(urls, value)
	}
	sort.Strings(urls)
	return urls
}
