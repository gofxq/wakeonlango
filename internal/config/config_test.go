package config

import "testing"

func TestValidateDevice(t *testing.T) {
	t.Parallel()

	device := Device{
		ID:        "pc-1",
		Name:      "Office",
		MAC:       "AA:BB:CC:DD:EE:FF",
		Broadcast: "192.168.1.255",
		Port:      9,
	}
	if err := ValidateDevice(device); err != nil {
		t.Fatalf("ValidateDevice() error = %v", err)
	}
}

func TestValidateDeviceInvalidMAC(t *testing.T) {
	t.Parallel()

	device := Device{
		ID:        "pc-1",
		Name:      "Office",
		MAC:       "invalid",
		Broadcast: "192.168.1.255",
		Port:      9,
	}
	if err := ValidateDevice(device); err == nil {
		t.Fatal("ValidateDevice() expected error")
	}
}
