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
		r := &Record{CommonAttrs: pipe.CommonAttrs{SrcAddr: ip(src), DstAddr: ip(dst), SrcName: src, DstName: dst, SrcPort: 51000, DstPort: 443, OBIIP: "198.51.100.9"}}
		r.Metrics.Initiator = initiator
		return r
	}

	inbound := flow("203.0.113.7", "10.0.0.5", InitiatorSrc)
	assert.Equal(t, pipe.PublicClientsV4, get(attr.SrcName, inbound))
	assert.Equal(t, pipe.PublicClientsV4, get(attr.SrcAddress, inbound))
	assert.Equal(t, "10.0.0.5", get(attr.DstName, inbound))

	reply := flow("10.0.0.5", "203.0.113.7", InitiatorDst)
	assert.Equal(t, pipe.PublicClientsV4, get(attr.DstName, reply), "the reply direction of an inbound connection")
	assert.Equal(t, "10.0.0.5", get(attr.SrcName, reply))

	egress := flow("10.0.0.5", "203.0.113.7", InitiatorSrc)
	assert.Equal(t, "203.0.113.7", get(attr.DstName, egress), "a public server keeps its address")

	guessed := flow("203.0.113.7", "10.0.0.5", 0)
	assert.Equal(t, "203.0.113.7", get(attr.SrcName, guessed), "a flow whose opening was not seen keeps its ends")

	host := flow("198.51.100.9", "203.0.113.8", InitiatorSrc)
	assert.Equal(t, "198.51.100.9", get(attr.SrcName, host), "the host's own address is never collapsed")
}
