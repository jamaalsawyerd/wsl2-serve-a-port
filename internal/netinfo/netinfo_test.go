package netinfo

import (
	"net/netip"
	"strings"
	"testing"
)

func TestDefaultRouteIface(t *testing.T) {
	route := `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
docker0	0011AC00	00000000	0001	0	0	0	0000FFFF	0	0	0
eth1	00000000	0101A8C0	0003	0	0	200	00000000	0	0	0
eth0	00000000	01D011AC	0003	0	0	100	00000000	0	0	0
eth0	00D011AC	00000000	0001	0	0	0	00F0FFFF	0	0	0
`
	if got := defaultRouteIface(strings.NewReader(route)); got != "eth0" {
		t.Errorf("defaultRouteIface = %q, want eth0", got)
	}
	if got := defaultRouteIface(strings.NewReader("Iface\tDestination\n")); got != "" {
		t.Errorf("no routes: got %q, want empty", got)
	}
}

func TestParseWindowsIPs(t *testing.T) {
	in := `[{"alias":"ZeroTier One","description":"ZeroTier Virtual Port","ip":"172.22.184.45","metric":10,"hardware":false},` +
		`{"alias":"Ethernet","description":"Realtek","ip":"192.168.1.5","metric":50,"hardware":true},` +
		`{"alias":"Wi-Fi","description":"AX211","ip":"192.168.1.50","metric":30,"hardware":true},` +
		`{"alias":"Broken","ip":"nope","metric":1,"hardware":true}]`
	got, err := parseWindowsIPs([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, a := range got {
		order = append(order, a.Alias)
	}
	if want := "Wi-Fi,Ethernet,ZeroTier One"; strings.Join(order, ",") != want {
		t.Errorf("order = %v, want %s", order, want)
	}

	for _, empty := range []string{"", "null", "[]"} {
		if got, err := parseWindowsIPs([]byte(empty)); err != nil || len(got) != 0 {
			t.Errorf("parseWindowsIPs(%q) = %v, %v", empty, got, err)
		}
	}
}

func TestParseHexAddr(t *testing.T) {
	tests := map[string]string{
		"0100007F":                         "127.0.0.1",
		"00000000":                         "0.0.0.0",
		"2FD511AC":                         "172.17.213.47",
		"FEFFFF0A":                         "10.255.255.254",
		"00000000000000000000000000000000": "::",
		"00000000000000000000000001000000": "::1",
		"0000000000000000FFFF00000100007F": "127.0.0.1", // v4-mapped
	}
	for in, want := range tests {
		got, err := parseHexAddr(in)
		if err != nil || got.String() != want {
			t.Errorf("parseHexAddr(%s) = %v, %v; want %s", in, got, err, want)
		}
	}
	if _, err := parseHexAddr("XYZ"); err == nil {
		t.Error("expected error for bad hex")
	}
}

const procTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: FEFFFF0A:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10254 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 222 1 0000000000000000 100 0 0 10 0
   2: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 333 1 0000000000000000 100 0 0 10 0
   3: 2FD511AC:0BB8 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 444 1 0000000000000000 100 0 0 10 0
   4: 2FD511AC:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 555 1 0000000000000000 100 0 0 10 0
`

const procTCP6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 666 1 0000000000000000 100 0 0 10 0
`

func TestListenersGrouping(t *testing.T) {
	v4, err := parseProcNetTCP(strings.NewReader(procTCP))
	if err != nil {
		t.Fatal(err)
	}
	v6, err := parseProcNetTCP(strings.NewReader(procTCP6))
	if err != nil {
		t.Fatal(err)
	}
	if len(v4) != 4 {
		t.Fatalf("got %d v4 listen sockets, want 4 (established one skipped)", len(v4))
	}

	wslIP := netip.MustParseAddr("172.17.213.47")
	procs := map[string]proc{"333": {name: "node", pid: 42}}
	got := groupListeners(append(v4, v6...), wslIP, procs)

	want := []struct {
		port      int
		reachable bool
		addrs     string
		process   string
	}{
		{53, false, "10.255.255.254", ""},
		{3000, true, "0.0.0.0", "node"},
		{5173, true, "172.17.213.47", ""},
		{8080, false, "127.0.0.1 ::1", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d listeners, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		var addrs []string
		for _, a := range g.Addrs {
			addrs = append(addrs, a.String())
		}
		if g.Port != w.port || g.Reachable != w.reachable || strings.Join(addrs, " ") != w.addrs || g.Process != w.process {
			t.Errorf("listener %d = %+v, want %+v", i, g, w)
		}
	}
}
