package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wakego/internal/config"
	"wakego/internal/scanner"
)

type fakeScanner struct {
	result scanner.Result
	err    error
}

func (f fakeScanner) Scan(context.Context, string) (scanner.Result, error) {
	return f.result, f.err
}

func TestIndexAndDevicesEndpoint(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	handler, err := New(Options{Store: store})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "WOL 控制台") {
		t.Fatal("GET / body missing title")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/devices status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload ok = %v, want true", payload["ok"])
	}
}

func TestAdminSaveDeviceEndpoint(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	handler, err := New(Options{Store: store})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	body := `{"name":"办公电脑","mac":"AA:BB:CC:DD:EE:FF","broadcast":"192.168.1.255","port":9,"remark":"工位A"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/device/save", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Password", "123456")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/device/save status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if got := len(store.ListDevices()); got != 1 {
		t.Fatalf("device count = %d, want 1", got)
	}
}

func TestAdminScanEndpoint(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.SaveDevice(config.Device{
		ID:        "pc-1",
		Name:      "Office",
		MAC:       "AA:BB:CC:DD:EE:FF",
		Broadcast: "192.168.1.255",
		Port:      9,
	})
	if err != nil {
		t.Fatalf("SaveDevice() error = %v", err)
	}

	handler, err := New(Options{
		Store: store,
		Scanner: fakeScanner{result: scanner.Result{
			CIDR:      "192.168.1.0/24",
			Broadcast: "192.168.1.255",
			Hosts: []scanner.Host{
				{IP: "192.168.1.10", MAC: "AA:BB:CC:DD:EE:FF", Hostname: "office"},
				{IP: "192.168.1.20", MAC: "11:22:33:44:55:66", Hostname: "nas"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/scan", strings.NewReader(`{"cidr":"192.168.1.0/24"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Password", "123456")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/scan status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Hosts []struct {
				MAC        string `json:"mac"`
				Configured bool   `json:"configured"`
			} `json:"hosts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !payload.OK || len(payload.Data.Hosts) != 2 {
		t.Fatalf("unexpected scan payload: %+v", payload)
	}
	if !payload.Data.Hosts[0].Configured {
		t.Fatal("expected first scanned host to be marked configured")
	}
}
