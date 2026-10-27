// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package clusterurl

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRouteCap_HeldRoutesPassAndNewOnesOverflowUntilIdle(t *testing.T) {
	start := time.Unix(1000, 0)
	now := start
	c := NewRouteCap(3, func() time.Time { return now })
	for i := range 3 {
		assert.Equal(t, "/r"+strconv.Itoa(i), c.Admit("/r"+strconv.Itoa(i)))
	}
	assert.Equal(t, OverflowRoute, c.Admit("/new"))
	assert.Equal(t, "/r1", c.Admit("/r1"))

	now = start.Add(4 * time.Minute)
	assert.Equal(t, "/r0", c.Admit("/r0"))
	assert.Equal(t, OverflowRoute, c.Admit("/new"), "no route is idle yet")

	// /r1 and /r2 are past the idle bound and free their room; /r0 was refreshed.
	now = start.Add(5*time.Minute + 30*time.Second)
	assert.Equal(t, "/new", c.Admit("/new"))
	assert.Equal(t, "/r2", c.Admit("/r2"))
	assert.Equal(t, OverflowRoute, c.Admit("/r3"))
	assert.Equal(t, "/r0", c.Admit("/r0"))
}

func TestRouteCap_ZeroAndNilAdmitEverything(t *testing.T) {
	var nilCap *RouteCap
	assert.Equal(t, "/a", nilCap.Admit("/a"))
	c := NewRouteCap(0, nil)
	for i := range 100 {
		assert.Equal(t, "/r"+strconv.Itoa(i), c.Admit("/r"+strconv.Itoa(i)))
	}
}

func TestRouteCap_AFloodHoldsMemoryAtTheBound(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewRouteCap(200, func() time.Time { return now })
	for i := range 100_000 {
		c.Admit("/x/" + strconv.Itoa(i))
	}
	assert.Len(t, c.seen, 200)
}
