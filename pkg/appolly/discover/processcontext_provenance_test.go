// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package discover

import (
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
