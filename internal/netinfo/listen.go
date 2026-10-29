package netinfo

import (
	"bufio"
	"cmp"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// tcpListen is the TCP_LISTEN state in /proc/net/tcp.
const tcpListen = "0A"

// Listener is a TCP port with at least one listening socket in WSL.
type Listener struct {
	Port  int
	Addrs []netip.Addr
	// Reachable is true when some socket is bound to a wildcard address or
	// to the WSL IP, so a Windows portproxy can connect to it. Servers bound
	// only to 127.0.0.1 are not reachable.
	Reachable bool
	// Process is the command name, when we're allowed to see it.
	Process string
	PID     int
}

type socket struct {
	addr  netip.Addr
	port  int
	inode string
}

// Listeners returns every listening TCP port in WSL, sorted by port.
func Listeners(wslIP netip.Addr) ([]Listener, error) {
	var socks []socket
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s, err := parseProcNetTCP(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		socks = append(socks, s...)
	}
	return groupListeners(socks, wslIP, processesByInode("/proc")), nil
}

func groupListeners(socks []socket, wslIP netip.Addr, procs map[string]proc) []Listener {
	byPort := make(map[int]*Listener)
	for _, s := range socks {
		l := byPort[s.port]
		if l == nil {
			l = &Listener{Port: s.port}
			byPort[s.port] = l
		}
		if !slices.Contains(l.Addrs, s.addr) {
			l.Addrs = append(l.Addrs, s.addr)
		}
		if s.addr.IsUnspecified() || s.addr == wslIP {
			l.Reachable = true
		}
		if p, ok := procs[s.inode]; ok && l.Process == "" {
			l.Process, l.PID = p.name, p.pid
		}
	}

	out := make([]Listener, 0, len(byPort))
	for _, l := range byPort {
		slices.SortFunc(l.Addrs, netip.Addr.Compare)
		out = append(out, *l)
	}
	slices.SortFunc(out, func(a, b Listener) int { return cmp.Compare(a.Port, b.Port) })
	return out
}

// parseProcNetTCP reads the listening sockets from /proc/net/tcp or tcp6.
func parseProcNetTCP(r io.Reader) ([]socket, error) {
	var out []socket
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[3] != tcpListen {
			continue
		}
		hexAddr, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		addr, err := parseHexAddr(hexAddr)
		if err != nil {
			return nil, err
		}
		port, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil {
			return nil, fmt.Errorf("bad port %q: %w", hexPort, err)
		}
		out = append(out, socket{addr: addr, port: int(port), inode: f[9]})
	}
	return out, sc.Err()
}

// parseHexAddr decodes the kernel's address format: the address as a series
// of host-order (little-endian) 32-bit words.
func parseHexAddr(s string) (netip.Addr, error) {
	b, err := hex.DecodeString(s)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return netip.Addr{}, fmt.Errorf("bad address %q", s)
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	addr, _ := netip.AddrFromSlice(b)
	return addr.Unmap(), nil
}

type proc struct {
	name string
	pid  int
}

// processesByInode maps socket inodes to the process holding them. Sockets
// owned by other users (e.g. root's docker-proxy) are silently skipped.
func processesByInode(procRoot string) map[string]proc {
	out := make(map[string]proc)
	fds, _ := filepath.Glob(filepath.Join(procRoot, "[0-9]*", "fd", "*"))
	for _, fd := range fds {
		link, err := os.Readlink(fd)
		if err != nil || !strings.HasPrefix(link, "socket:[") {
			continue
		}
		inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
		if _, seen := out[inode]; seen {
			continue
		}
		pidDir := filepath.Dir(filepath.Dir(fd))
		pid, _ := strconv.Atoi(filepath.Base(pidDir))
		comm, _ := os.ReadFile(filepath.Join(pidDir, "comm"))
		out[inode] = proc{name: strings.TrimSpace(string(comm)), pid: pid}
	}
	return out
}
