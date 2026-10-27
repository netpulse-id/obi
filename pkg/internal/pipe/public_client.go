// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipe // import "go.opentelemetry.io/obi/pkg/internal/pipe"

import "net"

const (
	// PublicClientsV4 is what the client end of a connection is reported as when it is a public IPv4 address.
	PublicClientsV4 = "0.0.0.0/0"
	// PublicClientsV6 is what the client end of a connection is reported as when it is a public IPv6 address.
	PublicClientsV6 = "::/0"
)

// sharedAddressSpace is the carrier-grade NAT range, which clusters and clouds use inside a network.
var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// PublicClient returns what the client end of a connection is reported as when it is a
// public address other than the host's own, so the connections of every public client of
// a host fold into one value per local end; false for any other address.
func PublicClient(ia *IPAddr, hostIP string) (string, bool) {
	ip := ia.IP()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || sharedAddressSpace.Contains(ip) || ip.String() == hostIP {
		return "", false
	}
	if ip.To4() != nil {
		return PublicClientsV4, true
	}
	return PublicClientsV6, true
}
