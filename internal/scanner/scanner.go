package scanner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"wakego/internal/config"
)

const (
	maxScanHosts  = 1024
	probeTimeout  = 250 * time.Millisecond
	scanDelay     = 300 * time.Millisecond
	workerCount   = 64
	lookupTimeout = 150 * time.Millisecond
)

var macPattern = regexp.MustCompile(`(?i)([0-9a-f]{2}[:-]){5}[0-9a-f]{2}`)

type Engine interface {
	Scan(context.Context, string) (Result, error)
}

type Result struct {
	CIDR      string `json:"cidr"`
	Broadcast string `json:"broadcast"`
	Hosts     []Host `json:"hosts"`
}

type Host struct {
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Hostname string `json:"hostname,omitempty"`
}

type ARPScanner struct{}

func New() *ARPScanner {
	return &ARPScanner{}
}

func (s *ARPScanner) Scan(ctx context.Context, cidr string) (Result, error) {
	network, hosts, err := expandCIDR(cidr)
	if err != nil {
		return Result{}, err
	}

	probeHosts(ctx, hosts)

	select {
	case <-time.After(scanDelay):
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	entries, err := readARPEntries()
	if err != nil {
		return Result{}, err
	}

	result := Result{
		CIDR:      network.String(),
		Broadcast: broadcastAddr(network).String(),
		Hosts:     make([]Host, 0, len(entries)),
	}
	for _, entry := range entries {
		ip := net.ParseIP(entry.IP)
		if ip == nil || ip.To4() == nil || !network.Contains(ip) {
			continue
		}

		host := Host{
			IP:       entry.IP,
			MAC:      strings.ToUpper(strings.ReplaceAll(entry.MAC, "-", ":")),
			Hostname: reverseLookup(ctx, entry.IP),
		}
		result.Hosts = append(result.Hosts, host)
	}

	sort.Slice(result.Hosts, func(i, j int) bool {
		return compareIPv4(result.Hosts[i].IP, result.Hosts[j].IP)
	})

	return result, nil
}

func SuggestedCIDRs(devices []config.Device) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 4)

	add := func(cidr string) {
		if cidr == "" {
			return
		}
		if _, ok := seen[cidr]; ok {
			return
		}
		seen[cidr] = struct{}{}
		out = append(out, cidr)
	}

	for _, device := range devices {
		add(cidrFromBroadcast(device.Broadcast))
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil || ipNet.IP.To4() == nil {
				continue
			}
			ip := ipNet.IP.To4()
			if !isPrivateIPv4(ip) {
				continue
			}
			add(fmt.Sprintf("%d.%d.%d.0/24", ip[0], ip[1], ip[2]))
		}
	}

	return out
}

type arpEntry struct {
	IP  string
	MAC string
}

func expandCIDR(raw string) (*net.IPNet, []net.IP, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, errors.New("cidr is required")
	}

	ip, network, err := net.ParseCIDR(raw)
	if err != nil {
		return nil, nil, errors.New("cidr must be a valid IPv4 network, e.g. 192.168.1.0/24")
	}
	if ip.To4() == nil {
		return nil, nil, errors.New("only IPv4 CIDR is supported")
	}

	ones, bits := network.Mask.Size()
	if bits != 32 {
		return nil, nil, errors.New("only IPv4 CIDR is supported")
	}

	hostCount := 1 << (bits - ones)
	if hostCount > maxScanHosts {
		return nil, nil, fmt.Errorf("cidr is too large, max %d addresses", maxScanHosts)
	}

	base := network.IP.Mask(network.Mask).To4()
	if base == nil {
		return nil, nil, errors.New("only IPv4 CIDR is supported")
	}

	ips := make([]net.IP, 0, hostCount)
	start := ipToUint32(base)
	end := start + uint32(hostCount) - 1
	if hostCount > 2 {
		start++
		end--
	}
	for current := start; current <= end; current++ {
		ips = append(ips, uint32ToIP(current))
	}

	return network, ips, nil
}

func probeHosts(ctx context.Context, ips []net.IP) {
	jobs := make(chan net.IP)
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				probeHost(ctx, ip)
			}
		}()
	}

	for _, ip := range ips {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- ip:
		}
	}
	close(jobs)
	wg.Wait()
}

func probeHost(ctx context.Context, ip net.IP) {
	dialCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp4", net.JoinHostPort(ip.String(), "80"))
	if err == nil && conn != nil {
		_ = conn.Close()
	}
}

func readARPEntries() ([]arpEntry, error) {
	if runtime.GOOS == "linux" {
		if entries, err := readProcARP("/proc/net/arp"); err == nil {
			return entries, nil
		}
	}

	if entries, err := runARPCommand("arp", "-an"); err == nil {
		return entries, nil
	}

	if runtime.GOOS == "linux" {
		if entries, err := runARPCommand("ip", "neigh"); err == nil {
			return entries, nil
		}
	}

	return nil, errors.New("unable to read ARP table")
}

func readProcARP(path string) ([]arpEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []arpEntry
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNo++
		if lineNo == 1 || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if fields[3] == "00:00:00:00:00:00" {
			continue
		}
		entries = append(entries, arpEntry{
			IP:  fields[0],
			MAC: fields[3],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func runARPCommand(name string, args ...string) ([]arpEntry, error) {
	output, err := exec.Command(name, args...).Output()
	if err != nil {
		return nil, err
	}
	if name == "ip" {
		return parseIPNeigh(output), nil
	}
	return parseARPOutput(output), nil
}

func parseARPOutput(output []byte) []arpEntry {
	lines := strings.Split(string(output), "\n")
	entries := make([]arpEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(strings.ToLower(line), "incomplete") {
			continue
		}

		ip := ""
		if start := strings.Index(line, "("); start >= 0 {
			if end := strings.Index(line[start:], ")"); end > 1 {
				ip = line[start+1 : start+end]
			}
		}
		if ip == "" {
			fields := strings.Fields(line)
			if len(fields) > 0 && net.ParseIP(fields[0]) != nil {
				ip = fields[0]
			}
		}

		mac := macPattern.FindString(line)
		if ip == "" || mac == "" {
			continue
		}

		entries = append(entries, arpEntry{IP: ip, MAC: mac})
	}
	return dedupeEntries(entries)
}

func parseIPNeigh(output []byte) []arpEntry {
	lines := strings.Split(string(output), "\n")
	entries := make([]arpEntry, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 5 || fields[1] != "dev" {
			continue
		}
		if !contains(fields, "lladdr") {
			continue
		}

		ip := fields[0]
		mac := ""
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "lladdr" {
				mac = fields[i+1]
				break
			}
		}
		if net.ParseIP(ip) == nil || mac == "" {
			continue
		}
		entries = append(entries, arpEntry{IP: ip, MAC: mac})
	}
	return dedupeEntries(entries)
}

func dedupeEntries(entries []arpEntry) []arpEntry {
	seen := make(map[string]struct{}, len(entries))
	out := make([]arpEntry, 0, len(entries))
	for _, entry := range entries {
		key := entry.IP + "|" + strings.ToUpper(entry.MAC)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}
	return out
}

func reverseLookup(ctx context.Context, ip string) string {
	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	names, err := net.DefaultResolver.LookupAddr(lookupCtx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

func broadcastAddr(network *net.IPNet) net.IP {
	base := network.IP.Mask(network.Mask).To4()
	mask := network.Mask
	return net.IPv4(
		base[0]|^mask[0],
		base[1]|^mask[1],
		base[2]|^mask[2],
		base[3]|^mask[3],
	)
}

func cidrFromBroadcast(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw)).To4()
	if ip == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.0/24", ip[0], ip[1], ip[2])
}

func isPrivateIPv4(ip net.IP) bool {
	return ip.IsPrivate()
}

func ipToUint32(ip net.IP) uint32 {
	v := ip.To4()
	return uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
}

func uint32ToIP(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func compareIPv4(a, b string) bool {
	return ipToUint32(net.ParseIP(a)) < ipToUint32(net.ParseIP(b))
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
