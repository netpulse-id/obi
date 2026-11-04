// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux && privileged_tests

package tpinjector // import "go.opentelemetry.io/obi/pkg/internal/ebpf/tpinjector"

import (
	"net"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/require"
)

// TestTCPOptionContextIsSampled sends a trace context in the TCP options of a loopback connection and checks that the receiving side records it sampled.
func TestTCPOptionContextIsSampled(t *testing.T) {
	objs := setupSockopsHarness(t)

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	client, err := net.Dial("tcp4", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	var server net.Conn
	select {
	case server = <-accepted:
	case <-time.After(cookieWaitTimeout):
		t.Fatal("timed out waiting for the accepted connection")
	}
	t.Cleanup(func() { server.Close() })

	sent := BpfTpInfoPidT{Valid: 1}
	sent.Tp.TraceId = [16]uint8{0x7f, 0x0c, 0x7d, 0x6d, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	sent.Tp.SpanId = [8]uint8{0x51, 1, 2, 3, 4, 5, 6, 7}

	raw, err := client.(*net.TCPConn).SyscallConn()
	require.NoError(t, err)
	var storeErr error
	require.NoError(t, raw.Control(func(fd uintptr) {
		key := uint32(fd)
		storeErr = objs.SkTpInfoPidMap.Update(&key, &sent, ebpf.UpdateAny)
	}))
	require.NoError(t, storeErr)

	_, err = client.Write([]byte("x"))
	require.NoError(t, err)
	buf := make([]byte, 1)
	require.NoError(t, server.SetReadDeadline(time.Now().Add(cookieWaitTimeout)))
	_, err = server.Read(buf)
	require.NoError(t, err)

	var received BpfTpInfoPidT
	waitFor(t, "the context parsed from the TCP options", func() bool {
		var key BpfConnectionInfoT
		var val BpfTpInfoPidT
		it := objs.IncomingTraceMap.Iterate()
		for it.Next(&key, &val) {
			if val.Tp.TraceId == sent.Tp.TraceId {
				received = val
				return true
			}
		}
		return false
	})

	require.Equal(t, sent.Tp.SpanId, received.Tp.SpanId)
	require.Equal(t, uint8(1), received.Tp.Flags, "a context continued from the TCP options is sampled")
}
