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
	"go.opentelemetry.io/obi/pkg/netolly/flowdef"
)

func TestRecordGetters_APublicClientSeenOpeningIsCollapsed(t *testing.T) {
	ip := func(s string) pipe.IPAddr {
		var ia pipe.IPAddr
		copy(ia[:], net.ParseIP(s).To16())
		return ia
	}
	getters := RecordStringGetters(RecordGettersConfig{PortGuessPolicy: flowdef.PortGuessOrdinal})
	get := func(name attr.Name, r *Record) string {
		g, ok := getters(name)
		require.True(t, ok)
		return g(r)
	}
	flow := func(src, dst string, initiator uint8) *Record {
		r := &Record{CommonAttrs: pipe.CommonAttrs{SrcAddr: ip(src), DstAddr: ip(dst), SrcName: src, DstName: dst, SrcPort: 51000, DstPort: 443, OBIIP: "9.9.9.9"}}
		r.Metrics.Initiator = initiator
		return r
	}

	inbound := flow("8.8.4.4", "10.0.0.5", InitiatorSrc)
	assert.Equal(t, pipe.PublicClientsV4, get(attr.SrcName, inbound))
	assert.Equal(t, pipe.PublicClientsV4, get(attr.SrcAddress, inbound))
	assert.Equal(t, "10.0.0.5", get(attr.DstName, inbound))

	reply := flow("10.0.0.5", "8.8.4.4", InitiatorDst)
	assert.Equal(t, pipe.PublicClientsV4, get(attr.DstName, reply), "the reply direction of an inbound connection")
	assert.Equal(t, "10.0.0.5", get(attr.SrcName, reply))

	egress := flow("10.0.0.5", "8.8.4.4", InitiatorSrc)
	assert.Equal(t, "8.8.4.4", get(attr.DstName, egress), "a public server keeps its address")

	guessed := flow("8.8.4.4", "10.0.0.5", 0)
	assert.Equal(t, "8.8.4.4", get(attr.SrcName, guessed), "a flow whose opening was not seen keeps its ends")

	pod := flow("2600:1f18:4a3:6b00::12", "2600:1f18:4a3:6b00::13", InitiatorSrc)
	assert.Equal(t, "2600:1f18:4a3:6b00::12", get(attr.SrcName, pod), "a pod with a global address calling another keeps its end")

	podEgress := flow("2600:1f18:4a3:6b00::12", "2606:4700::7", InitiatorSrc)
	assert.Equal(t, "2600:1f18:4a3:6b00::12", get(attr.SrcName, podEgress), "a pod's call to a public server keeps its end")

	host := flow("9.9.9.9", "8.8.8.8", InitiatorSrc)
	assert.Equal(t, "9.9.9.9", get(attr.SrcName, host), "the host's own address is never collapsed")
}
