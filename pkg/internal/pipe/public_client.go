// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipe // import "go.opentelemetry.io/obi/pkg/internal/pipe"

import (
	"net"
	"sync"
	"time"
)

const (
	// PublicClientsV4 is what the client end of a connection is reported as when it is a public IPv4 address.
	PublicClientsV4 = "0.0.0.0/0"
	// PublicClientsV6 is what the client end of a connection is reported as when it is a public IPv6 address.
	PublicClientsV6 = "::/0"

	// hostAddrsRefresh is how often the addresses of the host's own interfaces are read again.
	hostAddrsRefresh = 30 * time.Second
)

// notPublic are the global unicast ranges that never name a remote public client: shared
// address space, benchmarking, documentation and the NAT64 prefixes, behind which an
// internal address sits.
var notPublic = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48"} {
		_, n, _ := net.ParseCIDR(cidr)
		out = append(out, n)
	}
	return out
}()

// interfaceAddrs reads the addresses of the host's own interfaces.
var interfaceAddrs = net.InterfaceAddrs

var hostAddrs struct {
	mu   sync.Mutex
	set  map[string]struct{}
	read time.Time
}

// hostAddress reports whether an address is one of the host's own interfaces, read at most
// every hostAddrsRefresh.
func hostAddress(ip net.IP, now time.Time) bool {
	hostAddrs.mu.Lock()
	defer hostAddrs.mu.Unlock()
	if hostAddrs.set == nil || now.Sub(hostAddrs.read) >= hostAddrsRefresh {
		set := map[string]struct{}{}
		if addrs, err := interfaceAddrs(); err == nil {
			for _, a := range addrs {
				if n, ok := a.(*net.IPNet); ok {
					set[n.IP.String()] = struct{}{}
				}
			}
		}
		hostAddrs.set, hostAddrs.read = set, now
	}
	_, ok := hostAddrs.set[ip.String()]
	return ok
}

// remotePublic reports whether an address is a public address of no interface of this host.
func remotePublic(ip net.IP, hostIP string, now time.Time) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.String() == hostIP {
		return false
	}
	for _, n := range notPublic {
		if n.Contains(ip) {
			return false
		}
	}
	return !hostAddress(ip, now)
}

// localEnd reports whether an address is the server end of a connection to this host's
// workloads: a private or shared address, one of the host's own, or one of the local ranges.
func localEnd(ip net.IP, hostIP string, now time.Time, ranges localRangeSet) bool {
	return ip.IsPrivate() || notPublic[0].Contains(ip) || ip.String() == hostIP || hostAddress(ip, now) || ranges.contains(ip)
}

// PublicClient returns what the client end of a connection is reported as when it is a
// remote public address outside the local ranges calling a server end that is private, the
// host's own or local, so the connections of every public client of a host fold into one value
// per local end; false for any other pair, and for every IPv6 client unless the local ranges
// name every IPv6 range of this node, so a pod with a global address keeps its own end.
func PublicClient(client, server *IPAddr, hostIP string) (string, bool) {
	ip := client.IP()
	now := time.Now()
	ranges := currentLocalRanges(now)
	v4 := ip.To4() != nil
	// A global IPv6 address may be a pod's of this node, which only a complete set of local ranges tells from an internet client's.
	if !v4 && !ranges.ipv6Whole {
		return "", false
	}
	if !remotePublic(ip, hostIP, now) || !localEnd(server.IP(), hostIP, now, ranges) || ranges.contains(ip) {
		return "", false
	}
	if v4 {
		return PublicClientsV4, true
	}
	return PublicClientsV6, true
}
