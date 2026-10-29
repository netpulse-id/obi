// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package exec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.opentelemetry.io/otel/attribute"

	"go.opentelemetry.io/obi/pkg/appolly/app/svc"
	"go.opentelemetry.io/obi/pkg/appolly/discover/exec"
	"go.opentelemetry.io/obi/pkg/export/otel/otelcfg"
	"go.opentelemetry.io/obi/pkg/metadata"
)

// A process's own OTEL_RESOURCE_ATTRIBUTES, taken at discovery, never put a key that
// says where the telemetry came from on its resource; its other keys still arrive.
func TestApplyEnvVariables_TheProcessDeclaresNoProvenance(t *testing.T) {
	fi := exec.New(exec.Init{Service: svc.Attrs{UID: svc.UID{Name: "frontend", Namespace: "shop", Instance: "frontend-0"}, HostName: "node-1"}})
	fi.ApplyEnvVariables(map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "container.id=B,container.name=other,k8s.statefulset.name=forged," +
		"host.name=elsewhere,process.pid=1,service.instance.id=other-0,deployment.environment=canary"})
	service := fi.ServiceAttrs()

	got := map[string]string{}
	set := attribute.NewSet(otelcfg.GetAppResourceAttrs(&metadata.NodeMeta{HostID: "host-id"}, &service)...)
	for _, kv := range set.ToSlice() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	for _, key := range []string{"container.id", "container.name", "k8s.statefulset.name", "process.pid"} {
		assert.NotContains(t, got, key)
	}
	assert.Equal(t, "node-1", got["host.name"])
	assert.Equal(t, "frontend-0", got["service.instance.id"])
	assert.Equal(t, "canary", got["deployment.environment"])
}
