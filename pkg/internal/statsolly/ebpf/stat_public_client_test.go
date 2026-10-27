// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpf

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	attr "go.opentelemetry.io/obi/pkg/export/attributes/names"
	"go.opentelemetry.io/obi/pkg/internal/pipe"
)

func TestStatGetters_APublicClientOfAServerSocketIsCollapsed(t *testing.T) {
	ip := func(s string) pipe.IPAddr {
		var ia pipe.IPAddr
		copy(ia[:], net.ParseIP(s).To16())
		return ia
	}
	dst, ok := StatGetters(attr.DstAddress)
	require.True(t, ok)
	src, ok := StatGetters(attr.SrcAddress)
	require.True(t, ok)
	stat := func(local, remote string, role NetworkTCPHandshakeRoleCode) *Stat {
		return &Stat{
			Type:                    StatTypeTCPSuccessfulConnection,
			TCPSuccessfulConnection: &TCPSuccessfulConnection{Role: uint8(role)},
			CommonAttrs:             pipe.CommonAttrs{SrcAddr: ip(local), DstAddr: ip(remote), OBIIP: "9.9.9.9"},
		}
	}

	server := stat("10.0.0.5", "8.8.4.4", CodeRoleServer)
	assert.Equal(t, pipe.PublicClientsV4, dst(server).Value.AsString())
	assert.Equal(t, "10.0.0.5", src(server).Value.AsString())

	client := stat("10.0.0.5", "8.8.4.4", CodeRoleClient)
	assert.Equal(t, "8.8.4.4", dst(client).Value.AsString(), "a public server a socket dialed keeps its address")

	private := stat("10.0.0.5", "10.0.0.6", CodeRoleServer)
	assert.Equal(t, "10.0.0.6", dst(private).Value.AsString())

	v6 := stat("fd00::5", "2606:4700::7", CodeRoleServer)
	assert.Equal(t, "2606:4700::7", dst(v6).Value.AsString(), "an IPv6 client keeps its address")

	rtt := &Stat{Type: StatTypeTCPRtt, TCPRtt: &TCPRtt{Role: uint8(CodeRoleServer)}, CommonAttrs: pipe.CommonAttrs{SrcAddr: ip("10.0.0.5"), DstAddr: ip("8.8.4.4")}}
	assert.Equal(t, pipe.PublicClientsV4, dst(rtt).Value.AsString())

	pod := stat("2600:1f18:4a3:6b00::13", "2600:1f18:4a3:6b00::12", CodeRoleServer)
	assert.Equal(t, "2600:1f18:4a3:6b00::12", dst(pod).Value.AsString(), "a calling pod with a global address keeps it")

	retransmit := &Stat{Type: StatTypeTCPRetransmit, TCPRetransmit: true, CommonAttrs: pipe.CommonAttrs{SrcAddr: ip("10.0.0.5"), DstAddr: ip("8.8.4.4")}}
	assert.Equal(t, "8.8.4.4", dst(retransmit).Value.AsString(), "a stat without a role keeps its ends")
}
