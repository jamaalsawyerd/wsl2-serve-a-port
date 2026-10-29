// Package netinfo discovers the WSL and Windows IP addresses and the TCP
// ports listening inside WSL.
package netinfo

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// WSLAddr is the WSL VM's address on the virtual network Windows forwards to.
type WSLAddr struct {
	Interface string
	IP        netip.Addr
}

// WSLIP returns the IPv4 address of the interface holding the default route,
// which is eth0 on a stock WSL2 install. Using the route rather than picking
// "the first non-loopback address" avoids grabbing a Docker bridge.
func WSLIP() (WSLAddr, error) {
	iface := "eth0"
	if f, err := os.Open("/proc/net/route"); err == nil {
		if name := defaultRouteIface(f); name != "" {
			iface = name
		}
		f.Close()
	}

	ifc, err := net.InterfaceByName(iface)
	if err != nil {
		return WSLAddr{}, fmt.Errorf("looking up %s: %w", iface, err)
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return WSLAddr{}, fmt.Errorf("reading %s addresses: %w", iface, err)
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipnet.IP); ok && ip.Unmap().Is4() {
				return WSLAddr{Interface: iface, IP: ip.Unmap()}, nil
			}
		}
	}
	return WSLAddr{}, fmt.Errorf("%s has no IPv4 address", iface)
}

// defaultRouteIface parses /proc/net/route and returns the interface of the
// lowest-metric default route, or "".
func defaultRouteIface(r io.Reader) string {
	best, bestMetric := "", -1
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		metric, err := strconv.Atoi(f[6])
		if err != nil {
			continue
		}
		if bestMetric < 0 || metric < bestMetric {
			best, bestMetric = f[0], metric
		}
	}
	return best
}
