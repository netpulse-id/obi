// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package clusterurl // import "go.opentelemetry.io/obi/pkg/internal/transform/route/clusterurl"

import (
	"strings"
	"sync"
	"time"
)

const (
	// OverflowRoute is the route of a request whose own route found no room in its service's route cap.
	OverflowRoute = "/**"
	// RouteCapIdle is how long a route no request reached keeps its room.
	RouteCapIdle = 5 * time.Minute
	// routeCapSweepEvery is how often a full cap frees the routes past RouteCapIdle.
	routeCapSweepEvery = time.Minute
)

// RouteCap bounds the distinct routes one service reports: a route already held passes, a new one only while the cap holds room, and every other route becomes OverflowRoute.
type RouteCap struct {
	mu    sync.Mutex
	max   int
	now   func() time.Time
	seen  map[string]time.Time
	swept time.Time
}

// NewRouteCap returns a cap of max routes on the given clock; a max of zero or less admits every route.
func NewRouteCap(maxRoutes int, now func() time.Time) *RouteCap {
	if now == nil {
		now = time.Now
	}
	return &RouteCap{max: maxRoutes, now: now, seen: map[string]time.Time{}}
}

// Admit returns the route itself while the cap holds or has room for it, else OverflowRoute.
func (c *RouteCap) Admit(route string) string {
	if c == nil || c.max <= 0 {
		return route
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, held := c.seen[route]; held {
		c.seen[route] = now
		return route
	}
	if len(c.seen) >= c.max && now.Sub(c.swept) >= routeCapSweepEvery {
		c.swept = now
		for r, at := range c.seen {
			if now.Sub(at) >= RouteCapIdle {
				delete(c.seen, r)
			}
		}
	}
	if len(c.seen) >= c.max {
		return OverflowRoute
	}
	c.seen[strings.Clone(route)] = now
	return route
}
