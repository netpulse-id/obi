// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package attributes // import "go.opentelemetry.io/obi/pkg/export/attributes"

import (
	"slices"
	"strings"

	"go.opentelemetry.io/obi/pkg/internal/split"
)

type VarHandler func(k string, v string)

// provenanceKeyPrefixes and provenanceKeys name the resource keys that say where the
// telemetry came from and what produced it. OBI resolves them from the kernel, the cgroup
// and the Kubernetes API, or writes them itself, so a target's own declaration of one
// (its OTEL_RESOURCE_ATTRIBUTES, its process context, its pod's resource annotations or
// mapped labels) is never taken.
var (
	provenanceKeyPrefixes = []string{"container.", "k8s.", "host.", "process.", "telemetry.", "os."}
	provenanceKeys        = []string{"service.instance.id"}
)

// ProvenanceKey reports whether a resource key says where the telemetry came from.
func ProvenanceKey(key string) bool {
	if slices.Contains(provenanceKeys, key) {
		return true
	}
	for _, prefix := range provenanceKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func ParseOTELResourceVariable(envVar string, handler VarHandler) {
	variables := split.NewStringIterator(envVar, ",")

	for {
		variable, eof := variables.Next()

		if eof {
			break
		}

		variable = strings.TrimSuffix(variable, ",")

		key, value, found := strings.Cut(variable, "=")

		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" || value == "" {
			continue
		}

		handler(key, value)
	}
}
