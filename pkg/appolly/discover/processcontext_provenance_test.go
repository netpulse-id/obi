// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package discover

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.opentelemetry.io/ebpf-profiler/process/processcontext"
	"go.opentelemetry.io/otel/attribute"

	"go.opentelemetry.io/obi/pkg/appolly/app/svc"
	execpkg "go.opentelemetry.io/obi/pkg/appolly/discover/exec"
	attr "go.opentelemetry.io/obi/pkg/export/attributes/names"
	"go.opentelemetry.io/obi/pkg/export/otel/otelcfg"
	"go.opentelemetry.io/obi/pkg/metadata"
)

// The resource a process publishes in its OTEL_CTX mapping never puts a key that says
// where the telemetry came from or what produced it on its resource, and never replaces
// a value OBI resolved; its other keys still arrive, and the process may update them later.
func TestApplyContext_TheProcessDeclaresNoProvenance(t *testing.T) {
	fi := execpkg.New(execpkg.Init{Service: svc.Attrs{
		UID:      svc.UID{Name: "frontend", Namespace: "shop", Instance: "frontend-0"},
		HostName: "node-1",
		Metadata: map[attr.Name]string{
			attr.ContainerID:         "A",
			attr.K8sNamespaceName:    "ns1",
			attr.K8sPodName:          "api-0",
			"deployment.environment": "prod",
		},
	}})
	entry := &processEntry{fi: fi}
	pcd := processContextDecorator{log: pclog()}

	resource := func() map[string]string {
		service := fi.ServiceAttrs()
		got := map[string]string{}
		set := attribute.NewSet(otelcfg.GetAppResourceAttrs(&metadata.NodeMeta{HostID: "host-id"}, &service)...)
		for _, kv := range set.ToSlice() {
			got[string(kv.Key)] = kv.Value.Emit()
		}
		return got
	}

	entry.contextInfo = processcontext.Info{ResourceAttrs: attribute.NewSet(
		attribute.String("container.id", "B"), attribute.String("container.name", "other"),
		attribute.String("k8s.namespace.name", "ns2"), attribute.String("k8s.pod.name", "other-0"),
		attribute.String("host.name", "elsewhere"), attribute.String("process.pid", "1"),
		attribute.String("service.instance.id", "other-0"),
		attribute.String("telemetry.distro.version", "9.9.9"), attribute.String("telemetry.distro.name", "other"),
		attribute.String("telemetry.sdk.language", "forged"), attribute.String("telemetry.sdk.name", "forged"),
		attribute.String("telemetry.sdk.version", "0.0.1"), attribute.String("os.type", "windows"),
		attribute.String("deployment.environment", "canary"), attribute.String("team", "payments"))}
	pcd.applyContext(entry)

	got := resource()
	assert.Equal(t, "A", got["container.id"])
	assert.Equal(t, "ns1", got["k8s.namespace.name"])
	assert.Equal(t, "api-0", got["k8s.pod.name"])
	assert.Equal(t, "node-1", got["host.name"])
	assert.Equal(t, "frontend-0", got["service.instance.id"])
	assert.Equal(t, "prod", got["deployment.environment"])
	assert.Equal(t, "payments", got["team"])
	for _, key := range []string{"container.name", "process.pid"} {
		assert.NotContains(t, got, key)
	}
	assert.Equal(t, "linux", got["os.type"])
	for key, forged := range map[string]string{
		"telemetry.distro.version": "9.9.9", "telemetry.distro.name": "other",
		"telemetry.sdk.language": "forged", "telemetry.sdk.name": "forged", "telemetry.sdk.version": "0.0.1",
	} {
		assert.NotEqual(t, forged, got[key], key)
	}

	entry.contextInfo = processcontext.Info{ResourceAttrs: attribute.NewSet(
		attribute.String("deployment.environment", "canary"), attribute.String("team", "billing"))}
	pcd.applyContext(entry)

	got = resource()
	assert.Equal(t, "prod", got["deployment.environment"])
	assert.Equal(t, "billing", got["team"])
}

// A process that publishes new keys on every poll holds only the keys of its last context,
// within the per-process bounds, while the metadata OBI resolved and the values it replaced
// stay; a key the process stops publishing is removed.
func TestApplyContext_ARepublishingProcessHoldsOnlyItsCurrentKeys(t *testing.T) {
	fi := execpkg.New(execpkg.Init{Service: svc.Attrs{
		UID:      svc.UID{Name: "frontend", Namespace: "shop"},
		Metadata: map[attr.Name]string{attr.K8sNamespaceName: "ns1", "deployment.environment": "prod"},
	}})
	entry := &processEntry{fi: fi}
	pcd := processContextDecorator{log: pclog()}

	for poll := range 300 {
		kvs := []attribute.KeyValue{attribute.String("deployment.environment", "canary")}
		for i := range 200 {
			kvs = append(kvs, attribute.String(fmt.Sprintf("k%03d_%03d", poll, i), "v"))
		}
		entry.contextInfo = processcontext.Info{ResourceAttrs: attribute.NewSet(kvs...)}
		pcd.applyContext(entry)
	}

	metadata := fi.ServiceAttrs().Metadata
	assert.Len(t, metadata, maxContextKeys+1)
	assert.Equal(t, "ns1", metadata[attr.K8sNamespaceName])
	assert.Equal(t, "prod", metadata["deployment.environment"])
	assert.Equal(t, "v", metadata["k299_000"])
	assert.NotContains(t, metadata, "k298_000")
	assert.Len(t, entry.published, maxContextKeys-1)

	entry.contextInfo = processcontext.Info{ResourceAttrs: attribute.NewSet(attribute.String("team", "payments"))}
	pcd.applyContext(entry)
	assert.Equal(t, map[attr.Name]string{
		attr.K8sNamespaceName: "ns1", "deployment.environment": "prod", "team": "payments",
	}, fi.ServiceAttrs().Metadata)

	entry.contextInfo = processcontext.Info{}
	pcd.applyContext(entry)
	assert.Equal(t, map[attr.Name]string{attr.K8sNamespaceName: "ns1", "deployment.environment": "prod"}, fi.ServiceAttrs().Metadata)
	assert.Empty(t, entry.published)
}

// A process context is taken in key order up to the byte bound of keys and values, and a
// published key whose value OBI replaced since is left with OBI's value when the process
// stops publishing it.
func TestApplyContext_ByteBoundAndReplacedKeys(t *testing.T) {
	fi := execpkg.New(execpkg.Init{Service: svc.Attrs{UID: svc.UID{Name: "frontend"}}})
	entry := &processEntry{fi: fi}
	pcd := processContextDecorator{log: pclog()}

	large := strings.Repeat("x", 6<<10)
	entry.contextInfo = processcontext.Info{ResourceAttrs: attribute.NewSet(
		attribute.String("a", large), attribute.String("b", large), attribute.String("c", large),
		attribute.String("d", "small"), attribute.String("team", "payments"))}
	pcd.applyContext(entry)
	metadata := fi.ServiceAttrs().Metadata
	assert.Contains(t, metadata, attr.Name("a"))
	assert.Contains(t, metadata, attr.Name("b"))
	assert.NotContains(t, metadata, attr.Name("c"))
	assert.Equal(t, "small", metadata["d"])
	assert.Equal(t, "payments", metadata["team"])

	fi.UpdateMetadata(func(m map[attr.Name]string) { m["team"] = "resolved" })
	entry.contextInfo = processcontext.Info{}
	pcd.applyContext(entry)
	assert.Equal(t, map[attr.Name]string{"team": "resolved"}, fi.ServiceAttrs().Metadata)
}

// The underscore and mixed-case spellings of a provenance key a process publishes in its
// context or its OTEL_RESOURCE_ATTRIBUTES never reach its resource.
func TestApplyContext_NoSpellingOfAProvenanceKey(t *testing.T) {
	fi := execpkg.New(execpkg.Init{Service: svc.Attrs{
		UID:      svc.UID{Name: "frontend", Namespace: "shop", Instance: "frontend-0"},
		HostName: "node-1",
		Metadata: map[attr.Name]string{attr.ContainerID: "A", attr.K8sNamespaceName: "ns1"},
	}})
	fi.ApplyEnvVariables(map[string]string{
		"OTEL_RESOURCE_ATTRIBUTES": "k8s_namespace_name=bank,container_name=declared,container_id=B,host_name=elsewhere,K8s.pod.name=forged-0,team=payments",
	})
	entry := &processEntry{fi: fi, contextInfo: processcontext.Info{ResourceAttrs: attribute.NewSet(
		attribute.String("k8s_deployment_name", "ledger"), attribute.String("container_name", "declared"),
		attribute.String("Service_Instance_Id", "other-0"), attribute.String("tier", "web"))}}
	pcd := processContextDecorator{log: pclog()}
	pcd.applyContext(entry)

	service := fi.ServiceAttrs()
	got := map[string]string{}
	set := attribute.NewSet(otelcfg.GetAppResourceAttrs(&metadata.NodeMeta{HostID: "host-id"}, &service)...)
	for _, kv := range set.ToSlice() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	for _, key := range []string{
		"k8s_namespace_name", "container_name", "container_id", "host_name", "K8s.pod.name",
		"k8s_deployment_name", "Service_Instance_Id", "container.name", "k8s.pod.name",
	} {
		assert.NotContains(t, got, key)
	}
	assert.Equal(t, "ns1", got["k8s.namespace.name"])
	assert.Equal(t, "A", got["container.id"])
	assert.Equal(t, "node-1", got["host.name"])
	assert.Equal(t, "frontend-0", got["service.instance.id"])
	assert.Equal(t, "payments", got["team"])
	assert.Equal(t, "web", got["tier"])
}
