// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpfcommon

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/internal/ebpf/kafkaparser"
	"go.opentelemetry.io/obi/pkg/internal/largebuf"
)

// metadataManyTopics builds a v12 Metadata response naming n topics with fresh ids, each with no partition.
func metadataManyTopics(n, base int) []byte {
	out := []byte{0, 0, 0, 80, 2, 0, 0, 0, 0,
		0, 0, 0, 0, 3, 0, 0, 0, 1, 10, 108, 111, 99, 97, 108, 104, 111, 115, 116, 0, 0, 35, 132, 0, 0, 0, 0, 0, 2, 10, 108, 111, 99, 97, 108, 104,
		111, 115, 116, 0, 0, 35, 133, 6, 114, 97, 99, 107, 49, 0, 0, 0, 0, 0, 1}
	out = appendUvarint(out, uint64(n+1))
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("x%d", base+i)
		out = append(out, 0, 0)
		out = appendUvarint(out, uint64(len(name)+1))
		out = append(out, []byte(name)...)
		id := base + i + 1000
		out = append(out, 0xEE, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(id>>24), byte(id>>16), byte(id>>8), byte(id))
		out = append(out, 0, 1, 0, 0, 0, 0, 0)
	}
	return append(out, 0)
}

func learnMany(t testing.TB, cache *KafkaTopicNames, proc KafkaProcess, n, base int) {
	t.Helper()
	_, _, err := ProcessKafkaEvent(largebuf.NewLargeBufferFrom(scopeMetadataReq), largebuf.NewLargeBufferFrom(metadataManyTopics(n, base)), cache, nil, proc, BpfConnectionInfoT{S_port: 40001, D_port: 19092})
	require.NoError(t, err)
}

// fetchOf is scopeFetch for another topic id.
func fetchOf(id [16]byte) []byte {
	out := append([]byte(nil), scopeFetch...)
	copy(out[len(out)-17:], id[:])
	return out
}

var topic2ID = [16]byte{33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48}

func fetchedID(t *testing.T, cache *KafkaTopicNames, proc KafkaProcess, req []byte) string {
	t.Helper()
	res, _, err := ProcessKafkaEvent(largebuf.NewLargeBufferFrom(req), nil, cache, nil, proc, BpfConnectionInfoT{S_port: 40000, D_port: 9092})
	require.NoError(t, err)
	require.Len(t, res, 1)
	return res[0].Topic
}

// victimWithTwoNames returns names where a victim process learned topic1 and topic2.
func victimWithTwoNames(t *testing.T) (*KafkaTopicNames, KafkaProcess) {
	t.Helper()
	victim := KafkaProcess{Ns: 1, Pid: 100}
	cache := NewKafkaTopicNames(4096)
	learnMetadata(t, cache, "topic1", victim, BpfConnectionInfoT{S_port: 40000, D_port: 9092})
	cache.Add(victim, kafkaparser.UUID(topic2ID), "topic2")
	return cache, victim
}

// Another process's Metadata responses, however many, never evict a process's names: the busier namespace loses its own first.
func TestKafkaTopicNames_AnotherProcessesResponsesNeverEvictAProcessesNames(t *testing.T) {
	cache, victim := victimWithTwoNames(t)
	attacker := KafkaProcess{Ns: 2, Pid: 200}
	for b := 0; b < 40*500; b += 500 {
		learnMany(t, cache, attacker, 500, b)
	}

	require.Equal(t, "topic1", fetchedTopic(t, cache, victim, BpfConnectionInfoT{S_port: 40000, D_port: 9092}))
	require.Equal(t, "topic2", fetchedID(t, cache, victim, fetchOf(topic2ID)))
	assert.LessOrEqual(t, cache.Len(), 4096)
	assert.Equal(t, kafkaTopicNamesPerNamespace, cache.spaces[attacker.Ns].names.Len(), "a namespace holds at most its share")
}

// Thousands of short processes of one namespace, each naming one topic, hold one share together and never drain another namespace's names (security review agent-263-r6).
func TestKafkaTopicNames_ManyProcessesOfOneNamespaceHoldOneShare(t *testing.T) {
	cache, victim := victimWithTwoNames(t)
	for i := range 4200 {
		learnMany(t, cache, KafkaProcess{Ns: 2, Pid: uint32(1000 + i)}, 1, i)
	}

	require.Equal(t, "topic1", fetchedTopic(t, cache, victim, BpfConnectionInfoT{S_port: 40000, D_port: 9092}))
	require.Equal(t, "topic2", fetchedID(t, cache, victim, fetchOf(topic2ID)))
	assert.Equal(t, kafkaTopicNamesPerNamespace, cache.spaces[2].names.Len())
	assert.Len(t, cache.spaces, 2)
	lastID := 4199 + 1000
	name, ok := cache.Get(KafkaProcess{Ns: 2, Pid: 1000 + 4199}, kafkaparser.UUID{0xEE, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(lastID >> 24), byte(lastID >> 16), byte(lastID >> 8), byte(lastID)})
	assert.True(t, ok)
	assert.Equal(t, "x4199", name, "names stay keyed per process")
}

// Past the host total, the namespace holding the most loses its least recently used name.
func TestKafkaTopicNames_TheHostTotalTakesFromTheNamespaceHoldingTheMost(t *testing.T) {
	cache := NewKafkaTopicNames(8)
	uuid := func(i int) kafkaparser.UUID { return kafkaparser.UUID{byte(i)} }
	big, small, late := KafkaProcess{Ns: 1, Pid: 1}, KafkaProcess{Ns: 2, Pid: 1}, KafkaProcess{Ns: 3, Pid: 1}
	for i := range 6 {
		cache.Add(big, uuid(i), fmt.Sprintf("big%d", i))
	}
	cache.Add(small, uuid(0), "small0")
	cache.Add(small, uuid(1), "small1")
	cache.Add(late, uuid(0), "late0")

	assert.Equal(t, 8, cache.Len())
	_, oldest := cache.Get(big, uuid(0))
	assert.False(t, oldest, "the busiest namespace lost its oldest")
	for _, p := range []KafkaProcess{small, late} {
		_, ok := cache.Get(p, uuid(0))
		assert.True(t, ok)
	}
}

// A namespace none of whose processes names or resolves a topic for the idle bound is forgotten, so a reused pid starts empty.
func TestKafkaTopicNames_ANamespaceIdlePastTheBoundIsForgotten(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	cache := NewKafkaTopicNames(4096)
	cache.now = func() time.Time { return at }
	gone, live := KafkaProcess{Ns: 1, Pid: 1}, KafkaProcess{Ns: 2, Pid: 2}
	cache.Add(gone, kafkaparser.UUID{1}, "orders")
	cache.Add(live, kafkaparser.UUID{1}, "orders")

	at = at.Add(kafkaTopicNamesIdle - time.Minute)
	_, _ = cache.Get(live, kafkaparser.UUID{1})
	at = at.Add(2 * time.Minute)
	cache.Add(live, kafkaparser.UUID{2}, "payments")

	_, ok := cache.Get(gone, kafkaparser.UUID{1})
	assert.False(t, ok)
	_, ok = cache.Get(live, kafkaparser.UUID{1})
	assert.True(t, ok)
	assert.Equal(t, 2, cache.Len())
}

// Finding the namespace to evict from never scans the namespaces: a response costs about the same with the host total full of one-name namespaces as with none (security review agent-263-r6).
func TestKafkaTopicNames_EvictionCostDoesNotGrowWithTheNamespaces(t *testing.T) {
	cost := func(fill int) time.Duration {
		best := time.Duration(1 << 62)
		for range 3 {
			cache := NewKafkaTopicNames(4096)
			for i := range fill {
				cache.Add(KafkaProcess{Ns: uint32(10 + i), Pid: 1}, kafkaparser.UUID{0xAA, byte(i >> 8), byte(i)}, "y")
			}
			start := time.Now()
			for r := range 10 {
				learnMany(t, cache, KafkaProcess{Ns: 3, Pid: 7}, 500, r*500)
			}
			best = min(best, time.Since(start))
		}
		return best
	}
	empty, full := cost(0), cost(4096)
	t.Logf("10 responses of 500 topics: none held %v, 4096 one-name namespaces held %v", empty, full)
	assert.Less(t, full, 4*empty+20*time.Millisecond)
}
