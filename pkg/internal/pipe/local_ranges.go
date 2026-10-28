// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipe // import "go.opentelemetry.io/obi/pkg/internal/pipe"

import (
	"bufio"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// localRangesRefresh is how often the local ranges file is read again.
	localRangesRefresh = 30 * time.Second
	// maxLocalRangesBytes bounds the local ranges file read.
	maxLocalRangesBytes = 256 << 10
	// maxLocalRanges bounds the prefixes kept from the local ranges file.
	maxLocalRanges = 4096
	// completeIPv6 is the line saying the file names every IPv6 range local to this node.
	completeIPv6 = "complete ipv6"
)

// prefixSet holds prefixes of one family by length, so an address is looked up once per
// distinct length.
type prefixSet struct {
	byBits map[int]map[netip.Addr]struct{}
	bits   []int
}

// add records one masked prefix.
func (s *prefixSet) add(p netip.Prefix) {
	if s.byBits == nil {
		s.byBits = map[int]map[netip.Addr]struct{}{}
	}
	if s.byBits[p.Bits()] == nil {
		s.byBits[p.Bits()] = map[netip.Addr]struct{}{}
		s.bits = append(s.bits, p.Bits())
	}
	s.byBits[p.Bits()][p.Addr()] = struct{}{}
}

// contains reports whether an address of the set's family lies in one of its prefixes.
func (s *prefixSet) contains(a netip.Addr) bool {
	for _, bits := range s.bits {
		p, err := a.Prefix(bits)
		if err != nil {
			continue
		}
		if _, ok := s.byBits[bits][p.Addr()]; ok {
			return true
		}
	}
	return false
}

// localRangeSet holds the local prefixes of each family, and whether the IPv6 ones are every
// IPv6 range local to this node.
type localRangeSet struct {
	v4, v6    prefixSet
	ipv6Whole bool
}

var localRanges struct {
	mu   sync.Mutex
	path string
	set  localRangeSet
	read time.Time
}

// SetLocalRangesFile names the file of the address ranges local to this node, empty for none.
func SetLocalRangesFile(path string) {
	localRanges.mu.Lock()
	defer localRanges.mu.Unlock()
	localRanges.path, localRanges.set, localRanges.read = path, localRangeSet{}, time.Time{}
}

// currentLocalRanges returns the local ranges, read again from their file at most every
// localRangesRefresh; a file that cannot be read leaves no range.
func currentLocalRanges(now time.Time) localRangeSet {
	localRanges.mu.Lock()
	defer localRanges.mu.Unlock()
	if localRanges.path == "" {
		return localRangeSet{}
	}
	if !localRanges.read.IsZero() && now.Sub(localRanges.read) < localRangesRefresh {
		return localRanges.set
	}
	localRanges.read = now
	f, err := os.Open(localRanges.path)
	if err != nil {
		localRanges.set = localRangeSet{}
		return localRanges.set
	}
	defer f.Close()
	localRanges.set = parseLocalRanges(io.LimitReader(f, maxLocalRangesBytes))
	return localRanges.set
}

// parseLocalRanges reads one prefix or address per line, at most maxLocalRanges, and the line
// saying the IPv6 ranges are complete; anything else is skipped.
func parseLocalRanges(r io.Reader) localRangeSet {
	var set localRangeSet
	sc := bufio.NewScanner(r)
	for n := 0; sc.Scan() && n < maxLocalRanges; {
		line := strings.TrimSpace(sc.Text())
		if line == completeIPv6 {
			set.ipv6Whole = true
			continue
		}
		p, err := netip.ParsePrefix(line)
		if err != nil {
			a, aerr := netip.ParseAddr(line)
			if aerr != nil {
				continue
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		a := p.Addr().Unmap()
		p = netip.PrefixFrom(a, min(p.Bits(), a.BitLen())).Masked()
		if a.Is4() {
			set.v4.add(p)
		} else {
			set.v6.add(p)
		}
		n++
	}
	return set
}

// contains reports whether an address lies in one of the local ranges.
func (s localRangeSet) contains(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if a.Is4() {
		return s.v4.contains(a)
	}
	return s.v6.contains(a)
}
