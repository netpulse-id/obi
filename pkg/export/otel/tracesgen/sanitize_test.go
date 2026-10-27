// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tracesgen

import (
	"testing"

	expirable2 "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel/attribute"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"go.opentelemetry.io/obi/pkg/appolly/app/request"
	"go.opentelemetry.io/obi/pkg/appolly/app/svc"
	"go.opentelemetry.io/obi/pkg/metadata"
)

func TestGenerateTraces_InvalidUTF8KeepsTheBatch(t *testing.T) {
	cache := expirable2.NewLRU[svc.UID, []attribute.KeyValue](10, nil, 0)
	generate := func(name, path string) ptrace.Traces {
		s := request.Span{
			Type: request.EventTypeHTTP, Method: "GET", Path: path, Route: path, Status: 200,
			RequestStart: 100, Start: 100, End: 200,
			Service: svc.Attrs{UID: svc.UID{Name: name, Instance: name}},
		}
		return GenerateTracesWithAttributes(cache, &s.Service, nil, &metadata.NodeMeta{},
			[]TraceSpanAndAttributes{{Span: &s, Attributes: TraceAttributesSelector(&s, nil)}}, "obi")
	}
	batch := ptrace.NewTraces()
	generate("good", "/api/items").ResourceSpans().MoveAndAppendTo(batch.ResourceSpans())
	generate("victim", "/a\xff/b\xfe").ResourceSpans().MoveAndAppendTo(batch.ResourceSpans())

	data, err := (&ptrace.ProtoMarshaler{}).MarshalTraces(batch)
	require.NoError(t, err)
	var payload tracepb.TracesData
	require.NoError(t, proto.Unmarshal(data, &payload))
	require.Len(t, payload.ResourceSpans, 2)

	victim := payload.ResourceSpans[1].ScopeSpans[0].Spans[0]
	assert.Equal(t, "GET /a/b", victim.Name)
	for _, kv := range victim.Attributes {
		if kv.Key == "url.path" || kv.Key == "http.route" {
			assert.Equal(t, "/a/b", kv.Value.GetStringValue(), kv.Key)
		}
	}
}

func TestSanitizeTraces_KeysAndNestedValues(t *testing.T) {
	traces := ptrace.NewTraces()
	s := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	s.SetName("ok")
	s.Attributes().PutStr("x-h\xffdr", "v\xff")
	nested := s.Attributes().PutEmptySlice("list")
	nested.AppendEmpty().SetStr("a\xfeb")
	s.Status().SetMessage("bad\xff")
	e := s.Events().AppendEmpty()
	e.SetName("ev\xff")

	SanitizeTraces(traces)

	got, ok := s.Attributes().Get("x-hdr")
	require.True(t, ok)
	assert.Equal(t, "v", got.Str())
	_, ok = s.Attributes().Get("x-h\xffdr")
	assert.False(t, ok)
	list, _ := s.Attributes().Get("list")
	assert.Equal(t, "ab", list.Slice().At(0).Str())
	assert.Equal(t, "bad", s.Status().Message())
	assert.Equal(t, "ev", e.Name())

	data, err := (&ptrace.ProtoMarshaler{}).MarshalTraces(traces)
	require.NoError(t, err)
	require.NoError(t, proto.Unmarshal(data, &tracepb.TracesData{}))
}
