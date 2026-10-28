// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipe

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicClient_LocalRanges(t *testing.T) {
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("35.200.1.10"), Mask: net.CIDRMask(32, 32)}}, nil
	}
	hostAddrs.set = nil
	file := filepath.Join(t.TempDir(), "local-ranges")
	require.NoError(t, os.WriteFile(file, []byte(strings.Join([]string{
		"34.118.0.0/16",
		"35.200.1.11",
		"2600:1f18:4a3:6b00::/56",
		"not a prefix",
		"complete ipv6",
	}, "\n")), 0o600))
	SetLocalRangesFile(file)
	t.Cleanup(func() { SetLocalRangesFile("") })
	addr := func(s string) *IPAddr {
		var ia IPAddr
		copy(ia[:], net.ParseIP(s).To16())
		return &ia
	}

	for _, c := range []struct{ client, server, want string }{
		{"34.118.4.7", "35.200.1.10", ""},
		{"34.118.4.7", "34.118.9.2", ""},
		{"35.200.1.11", "34.118.9.2", ""},
		{"8.8.4.4", "34.118.9.2", PublicClientsV4},
		{"8.8.4.4", "10.0.0.5", PublicClientsV4},
		{"2600:1f18:4a3:6b00::12", "2600:1f18:4a3:6b00::13", ""},
		{"2600:1f18:4a3:6b00::12", "fd00::a", ""},
		{"2606:4700::7", "2600:1f18:4a3:6b00::13", PublicClientsV6},
		{"2606:4700::7", "fd00::5", PublicClientsV6},
		{"2606:4700::7", "2a00:1450::1", ""},
	} {
		got, ok := PublicClient(addr(c.client), addr(c.server), "35.200.1.10")
		assert.Equal(t, c.want, got, "%s to %s", c.client, c.server)
		assert.Equal(t, c.want != "", ok, "%s to %s", c.client, c.server)
	}
}

func TestPublicClient_IPv6StaysUnlessTheRangesAreComplete(t *testing.T) {
	hostAddrs.set = nil
	file := filepath.Join(t.TempDir(), "local-ranges")
	require.NoError(t, os.WriteFile(file, []byte("2600:1f18:4a3:6b00::/56\n"), 0o600))
	SetLocalRangesFile(file)
	t.Cleanup(func() { SetLocalRangesFile("") })
	var client, server IPAddr
	copy(client[:], net.ParseIP("2606:4700::7").To16())
	copy(server[:], net.ParseIP("fd00::5").To16())

	_, ok := PublicClient(&client, &server, "198.51.100.9")

	assert.False(t, ok)
}

func TestParseLocalRanges_Bounded(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxLocalRanges+10; i++ {
		b.WriteString("10.0.0.0/8\n")
	}
	b.WriteString("complete ipv6\n")

	set := parseLocalRanges(strings.NewReader(b.String()))

	assert.False(t, set.ipv6Whole, "lines past the bound are not read")
	assert.True(t, set.contains(net.ParseIP("10.1.2.3")))
	assert.True(t, parseLocalRanges(strings.NewReader("::ffff:10.0.0.1\n")).contains(net.ParseIP("10.0.0.1")))
}
