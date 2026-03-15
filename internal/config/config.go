package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const DefaultWOLPort = 9

type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MAC       string `json:"mac"`
	Broadcast string `json:"broadcast"`
	Port      int    `json:"port"`
	Remark    string `json:"remark"`
}

type Config struct {
	Title         string   `json:"title"`
	AdminPassword string   `json:"admin_password"`
	DefaultPort   int      `json:"default_port"`
	Devices       []Device `json:"devices"`
}

func DefaultConfig() Config {
	return Config{
		Title:         "WOL 控制台",
		AdminPassword: "123456",
		DefaultPort:   DefaultWOLPort,
		Devices:       []Device{},
	}
}

type Store struct {
	path string

	mu  sync.RWMutex
	cfg Config
}

func NewStore(path string) (*Store, error) {
	store := &Store{
		path: path,
		cfg:  DefaultConfig(),
	}

	if err := store.loadOrCreate(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneConfig(s.cfg)
}

func (s *Store) ListDevices() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()

	devices := make([]Device, len(s.cfg.Devices))
	copy(devices, s.cfg.Devices)
	return devices
}

func (s *Store) GetDevice(id string) (Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, device := range s.cfg.Devices {
		if device.ID == id {
			return device, true
		}
	}
	return Device{}, false
}

func (s *Store) SaveDevice(device Device) (Device, error) {
	device = normalizeDevice(device)
	if device.ID == "" {
		device.ID = generateID()
	}
	if err := ValidateDevice(device); err != nil {
		return Device{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == device.ID {
			s.cfg.Devices[i] = device
			found = true
			break
		}
	}
	if !found {
		s.cfg.Devices = append(s.cfg.Devices, device)
	}

	if err := s.persistLocked(); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (s *Store) DeleteDevice(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("device id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.cfg.Devices[:0]
	found := false
	for _, device := range s.cfg.Devices {
		if device.ID == id {
			found = true
			continue
		}
		next = append(next, device)
	}
	if !found {
		return errors.New("device not found")
	}
	s.cfg.Devices = append([]Device(nil), next...)

	return s.persistLocked()
}

func (s *Store) SaveSettings(next Config) (Config, error) {
	next.Title = strings.TrimSpace(next.Title)
	next.AdminPassword = strings.TrimSpace(next.AdminPassword)
	if next.Title == "" {
		return Config{}, errors.New("title is required")
	}
	if next.AdminPassword == "" {
		return Config{}, errors.New("admin password is required")
	}
	if next.DefaultPort == 0 {
		next.DefaultPort = DefaultWOLPort
	}
	if err := validatePort(next.DefaultPort); err != nil {
		return Config{}, fmt.Errorf("default port: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg.Title = next.Title
	s.cfg.AdminPassword = next.AdminPassword
	s.cfg.DefaultPort = next.DefaultPort

	if err := s.persistLocked(); err != nil {
		return Config{}, err
	}
	return cloneConfig(s.cfg), nil
}

func (s *Store) loadOrCreate() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil && filepath.Dir(s.path) != "." {
				return err
			}
			return s.persistLocked()
		}
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	cfg = normalizeConfig(cfg)
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}

func (s *Store) persistLocked() error {
	s.cfg = normalizeConfig(s.cfg)
	if err := ValidateConfig(s.cfg); err != nil {
		return err
	}

	if dir := filepath.Dir(s.path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func ValidateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(cfg.AdminPassword) == "" {
		return errors.New("admin password is required")
	}
	if err := validatePort(cfg.DefaultPort); err != nil {
		return fmt.Errorf("default port: %w", err)
	}

	seen := make(map[string]struct{}, len(cfg.Devices))
	for _, device := range cfg.Devices {
		if err := ValidateDevice(device); err != nil {
			return fmt.Errorf("device %s: %w", device.Name, err)
		}
		if _, ok := seen[device.ID]; ok {
			return fmt.Errorf("duplicate device id: %s", device.ID)
		}
		seen[device.ID] = struct{}{}
	}
	return nil
}

func ValidateDevice(device Device) error {
	if strings.TrimSpace(device.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(device.Name) == "" {
		return errors.New("name is required")
	}
	if _, err := normalizeMAC(device.MAC); err != nil {
		return fmt.Errorf("mac: %w", err)
	}
	if err := validateBroadcast(device.Broadcast); err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}
	if err := validatePort(device.Port); err != nil {
		return fmt.Errorf("port: %w", err)
	}
	return nil
}

func normalizeConfig(cfg Config) Config {
	if strings.TrimSpace(cfg.Title) == "" {
		cfg.Title = "WOL 控制台"
	}
	if cfg.DefaultPort == 0 {
		cfg.DefaultPort = DefaultWOLPort
	}

	devices := make([]Device, 0, len(cfg.Devices))
	for _, device := range cfg.Devices {
		device = normalizeDevice(device)
		if device.Port == 0 {
			device.Port = cfg.DefaultPort
		}
		devices = append(devices, device)
	}
	cfg.Devices = devices
	return cfg
}

func normalizeDevice(device Device) Device {
	device.ID = strings.TrimSpace(device.ID)
	device.Name = strings.TrimSpace(device.Name)
	device.MAC = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(device.MAC), "-", ":"))
	device.Broadcast = strings.TrimSpace(device.Broadcast)
	device.Remark = strings.TrimSpace(device.Remark)
	return device
}

func normalizeMAC(value string) (net.HardwareAddr, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("is required")
	}
	return net.ParseMAC(value)
}

func validateBroadcast(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("is required")
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return errors.New("must be a valid IP address")
	}
	if ip.To4() == nil {
		return errors.New("must be an IPv4 address")
	}
	return nil
}

func validatePort(value int) error {
	if value < 1 || value > 65535 {
		return errors.New("must be between 1 and 65535")
	}
	return nil
}

func generateID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err == nil {
		return "dev-" + hex.EncodeToString(buf)
	}
	return "dev-" + strconv.FormatInt(int64(os.Getpid()), 36)
}

func cloneConfig(cfg Config) Config {
	dup := cfg
	dup.Devices = make([]Device, len(cfg.Devices))
	copy(dup.Devices, cfg.Devices)
	return dup
}
