// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpfcommon

import (
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"

	"go.opentelemetry.io/obi/pkg/internal/ebpf/kafkaparser"
)

const (
	// kafkaTopicNamesPerNamespace bounds the topic names the processes of one pid
	// namespace (a container or a pod) hold together.
	kafkaTopicNamesPerNamespace = 512
	// kafkaTopicNamesIdle is how long a pid namespace none of whose processes names or
	// resolves a topic keeps its names: longer than a client's Metadata refresh (5 minutes
	// by default), so live consumers keep them, and a namespace that went quiet loses them.
	kafkaTopicNamesIdle = 15 * time.Minute
)

// processTopic keys a name by the process whose own Metadata response gave it, within its
// pid namespace, so a process resolves only the names its own responses gave.
type processTopic struct {
	pid  uint32
	uuid kafkaparser.UUID
}

type namespaceTopicNames struct {
	ns    uint32
	names *simplelru.LRU[processTopic, string]
	seen  time.Time
}

// KafkaTopicNames holds the KIP-516 topic names each process's own Metadata responses
// gave. The processes of one pid namespace share at most kafkaTopicNamesPerNamespace
// names, their least recently used leaving first, so however many processes a container
// runs it holds no more than one share. Past the host total the namespace holding the most
// loses its least recently used name, found through namespaces indexed by how many names
// they hold, never by scanning them. A namespace idle past kafkaTopicNamesIdle is forgotten.
type KafkaTopicNames struct {
	total        int
	perNamespace int
	size         int
	spaces       map[uint32]*namespaceTopicNames
	// bySize indexes the namespaces by how many names each holds; most is the largest
	// count any namespace may hold, the busiest found by walking down from it.
	bySize  []map[uint32]struct{}
	most    int
	now     func() time.Time
	sweptAt time.Time
}

// NewKafkaTopicNames returns names bounded by total on the host.
func NewKafkaTopicNames(total int) *KafkaTopicNames {
	per := min(kafkaTopicNamesPerNamespace, max(total, 1))
	bySize := make([]map[uint32]struct{}, per+1)
	for i := range bySize {
		bySize[i] = map[uint32]struct{}{}
	}
	return &KafkaTopicNames{
		total:        total,
		perNamespace: per,
		spaces:       map[uint32]*namespaceTopicNames{},
		bySize:       bySize,
		now:          time.Now,
	}
}

// Add keeps the name a process's Metadata response gave a topic id.
func (n *KafkaTopicNames) Add(proc KafkaProcess, uuid kafkaparser.UUID, name string) {
	if n == nil || n.total <= 0 {
		return
	}
	now := n.now()
	n.sweep(now)
	key := processTopic{pid: proc.Pid, uuid: uuid}
	s := n.spaces[proc.Ns]
	if s == nil {
		names, _ := simplelru.NewLRU[processTopic, string](n.perNamespace, nil)
		s = &namespaceTopicNames{ns: proc.Ns, names: names}
		n.spaces[proc.Ns] = s
		n.bySize[0][proc.Ns] = struct{}{}
	}
	s.seen = now
	if s.names.Contains(key) {
		s.names.Add(key, name)
		return
	}
	switch {
	case s.names.Len() >= n.perNamespace:
		n.dropOldest(s)
	case n.size >= n.total:
		if busiest := n.busiest(); busiest != nil {
			n.dropOldest(busiest)
		}
	}
	if n.spaces[proc.Ns] == nil {
		n.spaces[proc.Ns] = s
		n.bySize[s.names.Len()][proc.Ns] = struct{}{}
	}
	before := s.names.Len()
	s.names.Add(key, name)
	n.size++
	n.resized(s, before)
}

// Get returns the name a process's own Metadata responses gave a topic id.
func (n *KafkaTopicNames) Get(proc KafkaProcess, uuid kafkaparser.UUID) (string, bool) {
	if n == nil {
		return "", false
	}
	s := n.spaces[proc.Ns]
	if s == nil {
		return "", false
	}
	s.seen = n.now()
	return s.names.Get(processTopic{pid: proc.Pid, uuid: uuid})
}

// Len returns how many names are held on the host.
func (n *KafkaTopicNames) Len() int {
	if n == nil {
		return 0
	}
	return n.size
}

// busiest returns the namespace holding the most names, nil for none.
func (n *KafkaTopicNames) busiest() *namespaceTopicNames {
	for n.most > 0 && len(n.bySize[n.most]) == 0 {
		n.most--
	}
	for ns := range n.bySize[n.most] {
		return n.spaces[ns]
	}
	return nil
}

// dropOldest removes a namespace's least recently used name, and the namespace once it holds none.
func (n *KafkaTopicNames) dropOldest(s *namespaceTopicNames) {
	before := s.names.Len()
	if _, _, ok := s.names.RemoveOldest(); !ok {
		return
	}
	n.size--
	n.resized(s, before)
	if s.names.Len() == 0 {
		delete(n.bySize[0], s.ns)
		delete(n.spaces, s.ns)
	}
}

// resized moves a namespace in the size index after its count changed from before.
func (n *KafkaTopicNames) resized(s *namespaceTopicNames, before int) {
	after := s.names.Len()
	if after == before {
		return
	}
	delete(n.bySize[before], s.ns)
	n.bySize[after][s.ns] = struct{}{}
	n.most = max(n.most, after)
}

// sweep forgets every namespace idle past kafkaTopicNamesIdle, at most once a minute.
func (n *KafkaTopicNames) sweep(now time.Time) {
	if now.Sub(n.sweptAt) < time.Minute {
		return
	}
	n.sweptAt = now
	for ns, s := range n.spaces {
		if now.Sub(s.seen) >= kafkaTopicNamesIdle {
			n.size -= s.names.Len()
			delete(n.bySize[s.names.Len()], ns)
			delete(n.spaces, ns)
		}
	}
}
