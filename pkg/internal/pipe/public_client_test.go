// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipe

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublicClient(t *testing.T) {
	addr := func(s string) *IPAddr {
		var ia IPAddr
		copy(ia[:], net.ParseIP(s).To16())
		return &ia
	}
	for ip, want := range map[string]string{
		"203.0.113.7":  PublicClientsV4,
		"2001:db8::7":  PublicClientsV6,
		"10.0.0.5":     "",
		"172.16.3.4":   "",
		"192.168.1.2":  "",
		"100.120.75.5": "",
		"127.0.0.1":    "",
		"169.254.1.1":  "",
		"fd00::1":      "",
		"fe80::1":      "",
		"198.51.100.9": "",
	} {
		got, ok := PublicClient(addr(ip), "198.51.100.9")
		assert.Equal(t, want, got, ip)
		assert.Equal(t, want != "", ok, ip)
	}
}
