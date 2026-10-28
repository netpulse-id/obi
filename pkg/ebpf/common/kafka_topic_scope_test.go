// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpfcommon

import (
	"strings"
	"testing"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/internal/largebuf"
)

var scopeMetadataReq = []byte{0, 0, 0, 80, 0, 3, 0, 12, 2, 0, 0, 0, 0, 0, 0}

// scopeMetadataResp builds a v12 Metadata response naming topic id 01..10 as name, then a second topic.
func scopeMetadataResp(name string) []byte {
	out := []byte{0, 0, 0, 80, 2, 0, 0, 0, 0,
		0, 0, 0, 0, 3, 0, 0, 0, 1, 10, 108, 111, 99, 97, 108, 104, 111, 115, 116, 0, 0, 35, 132, 0, 0, 0, 0, 0, 2, 10, 108, 111, 99, 97, 108, 104,
		111, 115, 116, 0, 0, 35, 133, 6, 114, 97, 99, 107, 49, 0, 0, 0, 0, 0, 1, 3, 0, 0}
	out = appendUvarint(out, uint64(len(name)+1))
	out = append(out, []byte(name)...)
	out = append(out, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		0, 0, 0, 0,
		0, 0, 0, 7, 116, 111, 112, 105, 99, 50, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 0)
	return out
}

func appendUvarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

// scopeFetch is a v17 Fetch of topic id 01..10.
var scopeFetch = []byte{
	0, 0, 0, 80, 0, 1, 0, 17, 0, 0, 0, 179, 0, 26,
	99, 111, 110, 115, 117, 109, 101, 114, 45, 102, 114, 97, 117, 100, 45, 100,
	101, 116, 101, 99, 116, 105, 111, 110, 45, 49,
	0, 0, 0, 1, 244, 0, 0, 0, 1, 3, 32, 0, 0, 0,
	35, 105, 175, 157, 0, 0, 0, 134, 2,
	1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	1,
}

func fetchedTopic(t *testing.T, cache *simplelru.LRU[KafkaTopicKey, string], proc KafkaProcess, conn BpfConnectionInfoT) string {
	t.Helper()
	res, _, err := ProcessKafkaEvent(largebuf.NewLargeBufferFrom(scopeFetch), nil, cache, nil, proc, conn)
	require.NoError(t, err)
	require.Len(t, res, 1)
	return res[0].Topic
}

func learnMetadata(t *testing.T, cache *simplelru.LRU[KafkaTopicKey, string], name string, proc KafkaProcess, conn BpfConnectionInfoT) {
	t.Helper()
	_, _, err := ProcessKafkaEvent(largebuf.NewLargeBufferFrom(scopeMetadataReq), largebuf.NewLargeBufferFrom(scopeMetadataResp(name)), cache, nil, proc, conn)
	require.NoError(t, err)
}

// A Metadata response one process reads never renames the topic id another process fetches.
func TestKafkaTopicNames_AnotherProcessesMetadataRenamesNothing(t *testing.T) {
	victim, attacker := KafkaProcess{Ns: 1, Pid: 100}, KafkaProcess{Ns: 2, Pid: 200}
	victimConn := BpfConnectionInfoT{S_port: 40000, D_port: 9092}
	attackerConn := BpfConnectionInfoT{S_port: 40001, D_port: 19092}
	cache, _ := simplelru.NewLRU[KafkaTopicKey, string](1024, nil)

	learnMetadata(t, cache, "topic1", victim, victimConn)
	learnMetadata(t, cache, "evil-x", attacker, attackerConn)

	require.Equal(t, "topic1", fetchedTopic(t, cache, victim, victimConn))
	require.Equal(t, "evil-x", fetchedTopic(t, cache, attacker, attackerConn), "a process resolves its own responses' names")
	require.Equal(t, unknownTopicName, fetchedTopic(t, cache, KafkaProcess{Ns: 1, Pid: 101}, victimConn), "a process that read no Metadata knows no name")
}

// A Metadata topic name longer than Kafka accepts names nothing; one of exactly 249 bytes is learned.
func TestKafkaTopicNames_ANameLongerThanKafkaAcceptsNamesNothing(t *testing.T) {
	proc := KafkaProcess{Ns: 1, Pid: 100}
	conn := BpfConnectionInfoT{S_port: 40000, D_port: 9092}
	cache, _ := simplelru.NewLRU[KafkaTopicKey, string](1024, nil)

	_, _, _ = ProcessKafkaEvent(largebuf.NewLargeBufferFrom(scopeMetadataReq), largebuf.NewLargeBufferFrom(scopeMetadataResp(strings.Repeat("t", 250))), cache, nil, proc, conn)
	require.Zero(t, cache.Len())
	require.Equal(t, unknownTopicName, fetchedTopic(t, cache, proc, conn))

	learnMetadata(t, cache, strings.Repeat("t", 249), proc, conn)
	require.Equal(t, strings.Repeat("t", 249), fetchedTopic(t, cache, proc, conn))
}
