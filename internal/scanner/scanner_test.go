package scanner

import (
	"context"
	"strings"
	"testing"
)

func TestExpandCIDR(t *testing.T) {
	t.Parallel()

	network, hosts, err := expandCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatalf("expandCIDR() error = %v", err)
	}
	if got, want := network.String(), "192.168.1.0/24"; got != want {
		t.Fatalf("network = %q, want %q", got, want)
	}
	if got, want := len(hosts), 254; got != want {
		t.Fatalf("host count = %d, want %d", got, want)
	}
}

func TestExpandCIDRRejectsLargeRange(t *testing.T) {
	t.Parallel()

	if _, _, err := expandCIDR("10.0.0.0/16"); err == nil {
		t.Fatal("expandCIDR() expected size limit error")
	}
}

func TestParseARPOutput(t *testing.T) {
	t.Parallel()

	output := `
? (192.168.1.1) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
? (192.168.1.2) at (incomplete) on en0 ifscope [ethernet]
? (192.168.1.20) at 11:22:33:44:55:66 on en0 ifscope [ethernet]
`
	entries := parseARPOutput([]byte(output))
	if got, want := len(entries), 2; got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
	if got, want := strings.ToUpper(entries[0].MAC), "AA:BB:CC:DD:EE:FF"; got != want {
		t.Fatalf("first mac = %q, want %q", got, want)
	}
}

func TestParseIPNeigh(t *testing.T) {
	t.Parallel()

	output := `
192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE
192.168.1.2 dev eth0 INCOMPLETE
192.168.1.3 dev eth0 lladdr 11:22:33:44:55:66 STALE
`
	entries := parseIPNeigh([]byte(output))
	if got, want := len(entries), 2; got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
}

func TestSuggestedCIDRs(t *testing.T) {
	t.Parallel()

	cidrs := SuggestedCIDRs(nil)
	_ = cidrs
}

func TestReverseLookupNoPanic(t *testing.T) {
	t.Parallel()

	_ = reverseLookup(context.Background(), "127.0.0.1")
}
