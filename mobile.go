package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

const mobileDeviceHeader = "X-Tickets-Local-Device-Key"

type MobileDevice struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	ResponderID string     `json:"responder_id"`
	KeyHash     string     `json:"key_hash,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

type mobileEnrollment struct {
	ResponderID string
	ExpiresAt   time.Time
}

type MobileAccessStore struct {
	mu          sync.Mutex
	path        string
	devices     []MobileDevice
	enrollments map[string]mobileEnrollment
}

func OpenMobileAccessStore(dataDir string) (*MobileAccessStore, error) {
	store := &MobileAccessStore{path: filepath.Join(dataDir, "mobile-devices.json"), enrollments: make(map[string]mobileEnrollment)}
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mobile devices: %w", err)
	}
	if err := json.Unmarshal(data, &store.devices); err != nil {
		return nil, fmt.Errorf("decode mobile devices: %w", err)
	}
	return store, nil
}

func (store *MobileAccessStore) CreateEnrollment(responderID, hostURL string, now time.Time) (string, time.Time, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(hostURL))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
		return "", time.Time{}, "", validationError{"choose a valid Host LAN address"}
	}
	parsed.Path = "/"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	token, err := randomMobileSecret()
	if err != nil {
		return "", time.Time{}, "", err
	}
	expires := now.UTC().Add(10 * time.Minute)
	store.mu.Lock()
	store.pruneEnrollmentsLocked(now)
	store.enrollments[hashMobileSecret(token)] = mobileEnrollment{ResponderID: responderID, ExpiresAt: expires}
	store.mu.Unlock()
	query := parsed.Query()
	query.Set("view", "mobile")
	query.Set("enroll", token)
	parsed.RawQuery = query.Encode()
	enrollmentURL := parsed.String()
	png, err := qrcode.Encode(enrollmentURL, qrcode.Medium, 320)
	if err != nil {
		return "", time.Time{}, "", fmt.Errorf("create enrollment QR code: %w", err)
	}
	return enrollmentURL, expires, "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func (store *MobileAccessStore) Redeem(token, deviceName string, now time.Time) (MobileDevice, string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneEnrollmentsLocked(now)
	hash := hashMobileSecret(token)
	enrollment, ok := store.enrollments[hash]
	if !ok {
		return MobileDevice{}, "", errors.New("enrollment code is invalid, expired, or already used")
	}
	delete(store.enrollments, hash)
	key, err := randomMobileSecret()
	if err != nil {
		return MobileDevice{}, "", err
	}
	device := MobileDevice{
		ID:          newID("device"),
		Name:        defaultString(abbreviate(clean(deviceName), 100), "Mobile browser"),
		ResponderID: enrollment.ResponderID,
		KeyHash:     hashMobileSecret(key),
		CreatedAt:   now.UTC(),
	}
	store.devices = append(store.devices, device)
	if err := store.writeLocked(); err != nil {
		store.devices = store.devices[:len(store.devices)-1]
		return MobileDevice{}, "", err
	}
	return publicMobileDevice(device), key, nil
}

func (store *MobileAccessStore) Authorize(key string, now time.Time) (MobileDevice, bool) {
	provided := hashMobileSecret(key)
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.devices {
		if subtle.ConstantTimeCompare([]byte(store.devices[index].KeyHash), []byte(provided)) == 1 {
			seen := now.UTC()
			if store.devices[index].LastSeenAt == nil || seen.Sub(*store.devices[index].LastSeenAt) >= time.Minute {
				store.devices[index].LastSeenAt = &seen
				_ = store.writeLocked()
			}
			return publicMobileDevice(store.devices[index]), true
		}
	}
	return MobileDevice{}, false
}

func (store *MobileAccessStore) Devices() []MobileDevice {
	store.mu.Lock()
	defer store.mu.Unlock()
	devices := make([]MobileDevice, len(store.devices))
	for index, device := range store.devices {
		devices[index] = publicMobileDevice(device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].CreatedAt.After(devices[j].CreatedAt) })
	return devices
}

func (store *MobileAccessStore) Revoke(id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index, device := range store.devices {
		if device.ID == id {
			previous := append([]MobileDevice(nil), store.devices...)
			store.devices = append(store.devices[:index], store.devices[index+1:]...)
			if err := store.writeLocked(); err != nil {
				store.devices = previous
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

func (store *MobileAccessStore) pruneEnrollmentsLocked(now time.Time) {
	for hash, enrollment := range store.enrollments {
		if !enrollment.ExpiresAt.After(now) {
			delete(store.enrollments, hash)
		}
	}
}

func (store *MobileAccessStore) writeLocked() error {
	data, err := json.MarshalIndent(store.devices, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".mobile-devices-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, store.path)
}

func randomMobileSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate mobile credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashMobileSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func publicMobileDevice(device MobileDevice) MobileDevice {
	device.KeyHash = ""
	return device
}
