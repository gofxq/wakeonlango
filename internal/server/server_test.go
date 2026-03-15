package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wakego/internal/config"
)

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
