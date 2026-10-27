// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tracesgen // import "go.opentelemetry.io/obi/pkg/export/otel/tracesgen"

import (
	"unicode/utf8"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/obi/pkg/export/attributes"
)

// SanitizeTraces drops the invalid UTF-8 bytes of every span name, status message, event name, link and attribute text of a batch, so one span cannot make the whole batch fail its marshal.
func SanitizeTraces(traces ptrace.Traces) {
	rss := traces.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		sanitizeMap(rs.Resource().Attributes())
		sss := rs.ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			ss := sss.At(j)
			ss.Scope().SetName(attributes.SanitizeUTF8(ss.Scope().Name()))
			ss.Scope().SetVersion(attributes.SanitizeUTF8(ss.Scope().Version()))
			sanitizeMap(ss.Scope().Attributes())
			spans := ss.Spans()
			for k := 0; k < spans.Len(); k++ {
				sanitizeSpan(spans.At(k))
			}
		}
	}
}

func sanitizeSpan(s ptrace.Span) {
	s.SetName(attributes.SanitizeUTF8(s.Name()))
	s.TraceState().FromRaw(attributes.SanitizeUTF8(s.TraceState().AsRaw()))
	s.Status().SetMessage(attributes.SanitizeUTF8(s.Status().Message()))
	sanitizeMap(s.Attributes())
	events := s.Events()
	for i := 0; i < events.Len(); i++ {
		e := events.At(i)
		e.SetName(attributes.SanitizeUTF8(e.Name()))
		sanitizeMap(e.Attributes())
	}
	links := s.Links()
	for i := 0; i < links.Len(); i++ {
		l := links.At(i)
		l.TraceState().FromRaw(attributes.SanitizeUTF8(l.TraceState().AsRaw()))
		sanitizeMap(l.Attributes())
	}
}

// sanitizeMap drops the invalid UTF-8 bytes of every key and text value of a map, nested values included; a key that sanitizes onto another key keeps the later value.
func sanitizeMap(m pcommon.Map) {
	var badKeys []string
	m.Range(func(k string, v pcommon.Value) bool {
		if !utf8.ValidString(k) {
			badKeys = append(badKeys, k)
		}
		sanitizeValue(v)
		return true
	})
	for _, k := range badKeys {
		v, ok := m.Get(k)
		if !ok {
			continue
		}
		moved := pcommon.NewValueEmpty()
		v.CopyTo(moved)
		m.Remove(k)
		moved.CopyTo(m.PutEmpty(attributes.SanitizeUTF8(k)))
	}
}

func sanitizeValue(v pcommon.Value) {
	switch v.Type() {
	case pcommon.ValueTypeStr:
		if s := v.Str(); !utf8.ValidString(s) {
			v.SetStr(attributes.SanitizeUTF8(s))
		}
	case pcommon.ValueTypeMap:
		sanitizeMap(v.Map())
	case pcommon.ValueTypeSlice:
		sl := v.Slice()
		for i := 0; i < sl.Len(); i++ {
			sanitizeValue(sl.At(i))
		}
	}
}
