// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipe

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublicClient(t *testing.T) {
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("198.51.100.9"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("2a01:4f8:c17:1::1"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("203.0.114.9"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}
	hostAddrs.set = nil
	addr := func(s string) *IPAddr {
		var ia IPAddr
		copy(ia[:], net.ParseIP(s).To16())
		return &ia
	}
	for _, c := range []struct{ client, server, want string }{
		{"8.8.4.4", "10.0.0.5", PublicClientsV4},
		{"2606:4700::7", "fd00::5", ""},
		{"8.8.4.4", "203.0.114.9", PublicClientsV4},
		{"2606:4700::7", "2a01:4f8:c17:1::1", ""},
		{"2600:1f18:4a3:6b00::12", "fd00::a", ""},
		{"2600:1f18:4a3:6b00::12", "2a01:4f8:c17:1::1", ""},
		{"2600:1f18:4a3:6b00::12", "2600:1f18:4a3:6b00::13", ""},
		{"2600:1f18:4a3:6b00::12", "2606:4700::7", ""},
		{"8.8.4.4", "1.1.1.1", ""},
		{"64:ff9b::a00:5", "fd00::5", ""},
		{"198.18.0.1", "10.0.0.5", ""},
		{"100.120.75.5", "10.0.0.5", ""},
		{"10.0.0.6", "10.0.0.5", ""},
		{"203.0.114.9", "10.0.0.5", ""},
		{"2a01:4f8:c17:1::1", "fd00::5", ""},
		{"198.51.100.9", "10.0.0.5", ""},
	} {
		got, ok := PublicClient(addr(c.client), addr(c.server), "198.51.100.9")
		assert.Equal(t, c.want, got, "%s to %s", c.client, c.server)
		assert.Equal(t, c.want != "", ok, "%s to %s", c.client, c.server)
	}
}
