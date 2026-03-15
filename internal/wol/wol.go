package wol

import (
	"errors"
	"fmt"
	"net"
	"time"
)

const magicPacketLength = 102

func Send(mac, broadcast string, port int) error {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("parse mac: %w", err)
	}
	packet, err := BuildPacket(hw)
	if err != nil {
		return err
	}

	addr := &net.UDPAddr{
		IP:   net.ParseIP(broadcast),
		Port: port,
	}
	if addr.IP == nil || addr.IP.To4() == nil {
		return errors.New("invalid broadcast address")
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("dial udp: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("write packet: %w", err)
	}
	return nil
}

func BuildPacket(mac net.HardwareAddr) ([]byte, error) {
	if len(mac) != 6 {
		return nil, errors.New("mac must be 6 bytes")
	}

	packet := make([]byte, magicPacketLength)
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	for i := 6; i < magicPacketLength; i += len(mac) {
		copy(packet[i:], mac)
	}
	return packet, nil
}
