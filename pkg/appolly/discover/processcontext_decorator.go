// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package discover // import "go.opentelemetry.io/obi/pkg/appolly/discover"

import (
	"context"
	"log/slog"
	"maps"
	"time"

	"go.opentelemetry.io/ebpf-profiler/libpf"
	"go.opentelemetry.io/ebpf-profiler/process/processcontext"
	"go.opentelemetry.io/ebpf-profiler/remotememory"
	"go.opentelemetry.io/otel/attribute"

	"go.opentelemetry.io/obi/pkg/appolly/app"
	"go.opentelemetry.io/obi/pkg/appolly/app/svc"
	execpkg "go.opentelemetry.io/obi/pkg/appolly/discover/exec"
	"go.opentelemetry.io/obi/pkg/ebpf"
	"go.opentelemetry.io/obi/pkg/export/attributes"
	attr "go.opentelemetry.io/obi/pkg/export/attributes/names"
	"go.opentelemetry.io/obi/pkg/internal/procs"
	"go.opentelemetry.io/obi/pkg/pipe/msg"
	"go.opentelemetry.io/obi/pkg/pipe/swarm"
)

func pclog() *slog.Logger {
	return slog.With("component", "ProcessContextDecorator")
}

func ProcessContextDecoratorProvider(
	pollInterval time.Duration,
	input, output *msg.Queue[[]Event[ebpf.Instrumentable]],
) swarm.InstanceFunc {
	return func(_ context.Context) (swarm.RunFunc, error) {
		pcd := processContextDecorator{
			in:           input.Subscribe(msg.SubscriberName("ProcessContextDecorator")),
			out:          output,
			log:          pclog(),
			pollInterval: pollInterval,
			tracked:      make(map[app.PID]*processEntry),
		}
		return pcd.decorate, nil
	}
}

// processEntry holds per-process state needed for polling.
type processEntry struct {
	fi          *execpkg.FileInfo
	contextInfo processcontext.Info
	// resolved identifies the context attributes last applied, applied holds the
	// process-context keys taken from them, and published the values this decorator wrote
	// into the process metadata, so a later context update replaces or removes those and
	// nothing else.
	resolved  attribute.Distinct
	applied   map[attr.Name]string
	published map[attr.Name]string
}

// processContextDecorator enriches discovered processes with context information
// shared by applications through the OTEL_CTX environment mapping, allowing services
// to export resource attributes and metadata without direct instrumentation.
//
// Enrichment is attempted immediately on process creation and then repeated on
// every poll tick to handle SDKs that register the mapping after startup and to
// pick up context updates published by the process over its lifetime.
type processContextDecorator struct {
	in           <-chan []Event[ebpf.Instrumentable]
	out          *msg.Queue[[]Event[ebpf.Instrumentable]]
	log          *slog.Logger
	pollInterval time.Duration
	tracked      map[app.PID]*processEntry
}

func (pcd *processContextDecorator) decorate(ctx context.Context) {
	defer pcd.out.Close()

	var tickerC <-chan time.Time
	if pcd.pollInterval > 0 {
		ticker := time.NewTicker(pcd.pollInterval)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	pcd.log.Debug("starting node")
	for {
		select {
		case <-ctx.Done():
			pcd.log.Debug("context done, stopping node")
			return
		case evs, ok := <-pcd.in:
			if !ok {
				pcd.log.Debug("input channel closed, stopping node")
				return
			}
			for i := range evs {
				ev := &evs[i]
				switch ev.Type {
				case EventCreated:
					pcd.handleCreated(ev)
				case EventDeleted:
					delete(pcd.tracked, ev.Obj.FileInfo.Pid())
				}
			}
			pcd.out.SendCtx(ctx, evs)
		case <-tickerC:
			pcd.poll()
		}
	}
}

// handleCreated registers the process and attempts an immediate enrichment.
func (pcd *processContextDecorator) handleCreated(ev *Event[ebpf.Instrumentable]) {
	pid := ev.Obj.FileInfo.Pid()
	entry := &processEntry{fi: ev.Obj.FileInfo}
	pcd.tracked[pid] = entry
	pcd.pollEntry(pid, entry)
}

// poll checks all tracked processes for new or updated process context.
func (pcd *processContextDecorator) poll() {
	for pid, entry := range pcd.tracked {
		pcd.pollEntry(pid, entry)
	}
}

// pollEntry locates the OTEL_CTX mapping and resolves its latest resource attributes.
func (pcd *processContextDecorator) pollEntry(pid app.PID, entry *processEntry) {
	mappingAddr, _ := pcd.findOTELContextMapping(pid)
	rm := remotememory.NewProcessVirtualMemory(libpf.PID(pid))
	entry.contextInfo = processcontext.Resolve(
		uint64(mappingAddr), libpf.PID(pid), rm, entry.contextInfo, nil)
	pcd.applyContext(entry)
}

func (pcd *processContextDecorator) findOTELContextMapping(pid app.PID) (libpf.Address, bool) {
	maps, err := procs.FindLibMaps(pid)
	if err != nil {
		pcd.log.Debug("failed to read process maps", "pid", pid, "error", err)
		return 0, false
	}

	for _, m := range maps {
		if processcontext.IsContextMapping(m.Perms.Execute, m.Pathname) {
			return libpf.Address(m.StartAddr), true
		}
	}
	return 0, false
}

// Bounds of the process-context keys one process holds in its metadata.
const (
	maxContextKeys  = 128
	maxContextBytes = 16 << 10
)

// applyContext makes the process-context keys of a process's metadata those of its current
// context, without the keys that say where the telemetry came from: the process memory is
// the workload's own declaration and never names its container, pod, host or instance.
// A key the process stopped publishing is removed, a value OBI resolved is never replaced,
// and the context is taken in key order up to maxContextKeys keys and maxContextBytes bytes
// of keys and values, all in one update of the metadata and the service UID. A context
// whose attributes did not change since the last poll is not applied again.
func (pcd *processContextDecorator) applyContext(entry *processEntry) {
	resolved := entry.contextInfo.ResourceAttrs.Equivalent()
	if resolved == entry.resolved {
		return
	}
	entry.resolved = resolved
	current := pcd.contextAttributes(entry.contextInfo)
	if maps.Equal(current, entry.applied) {
		return
	}
	entry.applied = current
	published := make(map[attr.Name]string, len(current))
	entry.fi.UpdateMetadata(func(m map[attr.Name]string, uid *svc.UID) {
		for key, value := range entry.published {
			if _, still := current[key]; !still && m[key] == value {
				delete(m, key)
			}
		}
		for key, value := range current {
			if held, ok := m[key]; ok {
				if own, mine := entry.published[key]; !mine || own != held {
					continue
				}
			}
			m[key] = value
			published[key] = value
		}

		// Populate service UID from process context attributes, but only if not
		// already explicitly set. This allows process-level metadata to establish
		// the service identity while preserving any explicit configuration.
		if name, ok := published[attr.ServiceName]; ok && uid.Name == "" {
			uid.Name = name
		}
		if namespace, ok := published[attr.ServiceNamespace]; ok && uid.Namespace == "" {
			uid.Namespace = namespace
		}
	})
	entry.published = published
}

// contextAttributes returns the string attributes of a process context a process may
// declare, in key order within the per-process bounds.
func (pcd *processContextDecorator) contextAttributes(info processcontext.Info) map[attr.Name]string {
	out := map[attr.Name]string{}
	size := 0
	for _, kv := range info.ResourceAttrs.ToSlice() {
		if len(out) == maxContextKeys {
			break
		}
		if attributes.ProvenanceKey(string(kv.Key)) {
			continue
		}
		if kv.Value.Type() != attribute.STRING {
			pcd.log.Debug("attribute value is not a string type", "type", kv.Value.Type())
			continue
		}
		value := kv.Value.AsString()
		if value == "" || size+len(kv.Key)+len(value) > maxContextBytes {
			continue
		}
		size += len(kv.Key) + len(value)
		out[attr.Name(kv.Key)] = value
	}
	return out
}
