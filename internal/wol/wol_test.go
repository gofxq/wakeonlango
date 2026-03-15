package wol

import (
	"bytes"
	"net"
	"testing"
)

func TestBuildPacket(t *testing.T) {
	t.Parallel()

	packet, err := BuildPacket(net.HardwareAddr{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
	if err != nil {
		t.Fatalf("BuildPacket() error = %v", err)
	}
	if len(packet) != 102 {
		t.Fatalf("packet length = %d, want 102", len(packet))
	}
	if !bytes.Equal(packet[:6], bytes.Repeat([]byte{0xFF}, 6)) {
		t.Fatal("packet prefix mismatch")
	}
}
