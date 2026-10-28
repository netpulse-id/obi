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

// Another process's Metadata responses, however many, never evict a process's names: the busier one loses its own first.
func TestKafkaTopicNames_AnotherProcessesResponsesNeverEvictAProcessesNames(t *testing.T) {
	victim, attacker := KafkaProcess{Ns: 1, Pid: 100}, KafkaProcess{Ns: 2, Pid: 200}
	conn := BpfConnectionInfoT{S_port: 40000, D_port: 9092}
	cache := NewKafkaTopicNames(4096)
	learnMetadata(t, cache, "topic1", victim, conn)

	for b := 0; b < 20*500; b += 500 {
		_, _, err := ProcessKafkaEvent(largebuf.NewLargeBufferFrom(scopeMetadataReq), largebuf.NewLargeBufferFrom(metadataManyTopics(500, b)), cache, nil, attacker, conn)
		require.NoError(t, err)
	}

	require.Equal(t, "topic1", fetchedTopic(t, cache, victim, conn))
	assert.LessOrEqual(t, cache.Len(), 4096)
	assert.Equal(t, kafkaTopicNamesPerProcess, cache.procs[attacker].names.Len(), "a process holds at most its share")
}

// Past the host total, the process holding the most loses its least recently used name.
func TestKafkaTopicNames_TheHostTotalTakesFromTheProcessHoldingTheMost(t *testing.T) {
	cache := NewKafkaTopicNames(8)
	uuid := func(i int) kafkaparser.UUID { return kafkaparser.UUID{byte(i)} }
	big, small, late := KafkaProcess{Pid: 1}, KafkaProcess{Pid: 2}, KafkaProcess{Pid: 3}
	for i := range 6 {
		cache.Add(big, uuid(i), fmt.Sprintf("big%d", i))
	}
	cache.Add(small, uuid(0), "small0")
	cache.Add(small, uuid(1), "small1")
	cache.Add(late, uuid(0), "late0")

	assert.Equal(t, 8, cache.Len())
	_, oldest := cache.Get(big, uuid(0))
	assert.False(t, oldest, "the busiest process lost its oldest")
	for _, p := range []KafkaProcess{small, late} {
		name, ok := cache.Get(p, uuid(0))
		assert.True(t, ok)
		assert.NotEmpty(t, name)
	}
}

// A process neither naming nor resolving a topic for the idle bound is forgotten, so a reused pid starts empty.
func TestKafkaTopicNames_AProcessIdlePastTheBoundIsForgotten(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	cache := NewKafkaTopicNames(4096)
	cache.now = func() time.Time { return at }
	gone, live := KafkaProcess{Pid: 1}, KafkaProcess{Pid: 2}
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
