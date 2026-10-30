// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package attributes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const resAttrs = "service.name=order-api, ,service.version=2.0.0,deployment.environment=staging=a"

func TestParseOTELResourceVariable(t *testing.T) {
	expected := map[string]string{
		"service.name":           "order-api",
		"service.version":        "2.0.0",
		"deployment.environment": "staging=a",
	}

	handler := func(k, v string) {
		require.NotEmpty(t, k)
		require.NotEmpty(t, v)

		e, ok := expected[k]

		require.True(t, ok)
		require.Equal(t, e, v)
	}

	ParseOTELResourceVariable(resAttrs, handler)
}

func TestParseOTELResourceVariable_NoAllocs(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		ParseOTELResourceVariable(resAttrs, func(_, _ string) {})
	})

	if allocs != 0 {
		t.Errorf("ParseOTELResourceVariable allocated %v allocs per run; want 0", allocs)
	}
}

func BenchmarkParseOTELResourceVariable(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		ParseOTELResourceVariable(resAttrs, func(_, _ string) {
			// no‑op handler
		})
	}
}

// Every spelling of a provenance key that folds to it is a provenance key, and a key
// that only starts like one is not.
func TestProvenanceKey_EverySpelling(t *testing.T) {
	for _, key := range []string{
		"k8s.namespace.name", "k8s_namespace_name", "k8s_namespace.name", "K8s.pod.name", "K8S_POD_NAME",
		"container.name", "container_name", "Container.ID", "container_id", " container.id ",
		"host.name", "host_name", "process.pid", "process_pid", "telemetry.sdk.name", "telemetry_distro_version",
		"os.type", "os_type", "service.instance.id", "service_instance_id", "Service.Instance_Id",
	} {
		require.True(t, ProvenanceKey(key), key)
	}
	for _, key := range []string{
		"service.name", "service_name", "service.namespace", "deployment.environment", "team",
		"containers", "k8s", "hostname", "host", "process", "telemetry", "osname", "service.instance",
	} {
		require.False(t, ProvenanceKey(key), key)
	}
}
