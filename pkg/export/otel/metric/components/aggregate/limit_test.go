// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package aggregate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestLimiterOverflowKeepsMethodAndOutcome(t *testing.T) {
	l := newLimiter[int](3)
	measurements := map[attribute.Distinct]int{}
	add := func(attrs ...attribute.KeyValue) attribute.Set {
		set := l.Attributes(attribute.NewSet(attrs...), measurements)
		measurements[set.Equivalent()]++
		return set
	}
	add(attribute.String("http.route", "/a"), attribute.Int("http.response.status_code", 200))
	add(attribute.String("http.route", "/b"), attribute.Int("http.response.status_code", 200))

	got := add(attribute.String("http.route", "/c"), attribute.String("http.request.method", "GET"), attribute.Int("http.response.status_code", 500))
	assert.Equal(t, attribute.NewSet(
		attribute.Bool("otel.metric.overflow", true),
		attribute.String("http.request.method", "GET"),
		attribute.Int("http.response.status_code", 500),
	), got)

	again := add(attribute.String("http.route", "/d"), attribute.String("http.request.method", "GET"), attribute.Int("http.response.status_code", 500))
	assert.Equal(t, got.Equivalent(), again.Equivalent(), "requests of one method and outcome share one overflow stream")

	assert.Equal(t, overflowSet, add(attribute.String("src.address", "203.0.113.7")), "a measurement with none of the kept attributes overflows into the plain set")
	assert.Len(t, measurements, 4)
}
