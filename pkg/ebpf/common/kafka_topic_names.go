// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpfcommon

import (
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"

	"go.opentelemetry.io/obi/pkg/internal/ebpf/kafkaparser"
)

const (
	// kafkaTopicNamesPerProcess bounds the topic names one process holds.
	kafkaTopicNamesPerProcess = 512
	// kafkaTopicNamesIdle is how long a process that neither names nor resolves a topic
	// keeps its names: longer than a client's Metadata refresh (5 minutes by default), so a
	// live consumer keeps them, and a process that exited loses them.
	kafkaTopicNamesIdle = 15 * time.Minute
)

// KafkaTopicNames holds the KIP-516 topic names each process's own Metadata responses
// gave. Every process has its own share of at most kafkaTopicNamesPerProcess names, its
// least recently used leaving first; past the total, the process holding the most loses
// its least recently used name, so one process's responses never evict another's names
// while it holds no more than they do. A process idle past kafkaTopicNamesIdle is forgotten.
type KafkaTopicNames struct {
	total      int
	perProcess int
	size       int
	procs      map[KafkaProcess]*processTopicNames
	now        func() time.Time
	sweptAt    time.Time
}

type processTopicNames struct {
	names *simplelru.LRU[kafkaparser.UUID, string]
	seen  time.Time
}

// NewKafkaTopicNames returns names bounded by total on the host.
func NewKafkaTopicNames(total int) *KafkaTopicNames {
	return &KafkaTopicNames{
		total:      total,
		perProcess: min(kafkaTopicNamesPerProcess, total),
		procs:      map[KafkaProcess]*processTopicNames{},
		now:        time.Now,
	}
}

// Add keeps the name a process's Metadata response gave a topic id.
func (n *KafkaTopicNames) Add(proc KafkaProcess, uuid kafkaparser.UUID, name string) {
	if n == nil || n.total <= 0 {
		return
	}
	now := n.now()
	n.sweep(now)
	p := n.procs[proc]
	if p == nil {
		names, _ := simplelru.NewLRU[kafkaparser.UUID, string](n.perProcess, nil)
		p = &processTopicNames{names: names}
		n.procs[proc] = p
	}
	p.seen = now
	if p.names.Contains(uuid) {
		p.names.Add(uuid, name)
		return
	}
	switch {
	case p.names.Len() >= n.perProcess:
		p.names.RemoveOldest()
		n.size--
	case n.size >= n.total:
		n.dropFromBusiest()
	}
	n.procs[proc] = p
	p.names.Add(uuid, name)
	n.size++
}

// Get returns the name a process's own Metadata responses gave a topic id.
func (n *KafkaTopicNames) Get(proc KafkaProcess, uuid kafkaparser.UUID) (string, bool) {
	if n == nil {
		return "", false
	}
	p := n.procs[proc]
	if p == nil {
		return "", false
	}
	p.seen = n.now()
	return p.names.Get(uuid)
}

// Len returns how many names are held on the host.
func (n *KafkaTopicNames) Len() int {
	if n == nil {
		return 0
	}
	return n.size
}

// dropFromBusiest removes the least recently used name of the process holding the most.
func (n *KafkaTopicNames) dropFromBusiest() {
	var busiest KafkaProcess
	var most *processTopicNames
	for proc, p := range n.procs {
		if most == nil || p.names.Len() > most.names.Len() {
			busiest, most = proc, p
		}
	}
	if most == nil {
		return
	}
	if _, _, ok := most.names.RemoveOldest(); ok {
		n.size--
	}
	if most.names.Len() == 0 {
		delete(n.procs, busiest)
	}
}

// sweep forgets every process idle past kafkaTopicNamesIdle, at most once a minute.
func (n *KafkaTopicNames) sweep(now time.Time) {
	if now.Sub(n.sweptAt) < time.Minute {
		return
	}
	n.sweptAt = now
	for proc, p := range n.procs {
		if now.Sub(p.seen) >= kafkaTopicNamesIdle {
			n.size -= p.names.Len()
			delete(n.procs, proc)
		}
	}
}
