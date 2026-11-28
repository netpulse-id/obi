// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package exec

import (
	"reflect"
	"testing"

	attr "go.opentelemetry.io/obi/pkg/export/attributes/names"
)

func TestApplyEnvVariables(t *testing.T) {
	tests := []struct {
		name       string
		envVars    map[string]string
		expectName string
		expectNS   string
		expectMeta map[attr.Name]string
		expectEnv  map[string]string
	}{
		{
			name:       "OTEL_SERVICE_NAME present, but also name is in the OTEL_RESOURCE_ATTRIBUTES",
			envVars:    map[string]string{"OTEL_SERVICE_NAME": "my-service", "OTEL_RESOURCE_ATTRIBUTES": "service.name=otel-svc,label1=1,label2=2"},
			expectName: "my-service",
			expectMeta: map[attr.Name]string{"label1": "1", "label2": "2", "service.name": "otel-svc"},
		},
		{
			name:       "OTEL_SERVICE_NAME present",
			envVars:    map[string]string{"OTEL_SERVICE_NAME": "my-service"},
			expectName: "my-service",
			expectNS:   "",
			expectMeta: map[attr.Name]string{},
		},
		{
			name:       "OTEL_RESOURCE_ATTRIBUTES with service.name",
			envVars:    map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "service.name=otel-svc"},
			expectName: "otel-svc",
			expectMeta: map[attr.Name]string{"service.name": "otel-svc"},
		},
		{
			name:       "OTEL_RESOURCE_ATTRIBUTES with service.name and service.namespace",
			envVars:    map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "service.name=otel-svc,service.namespace=ns1"},
			expectName: "otel-svc",
			expectNS:   "ns1",
			expectMeta: map[attr.Name]string{"service.name": "otel-svc", "service.namespace": "ns1"},
		},
		{
			name:       "OTEL_RESOURCE_ATTRIBUTES with service.namespace",
			envVars:    map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "service.namespace=otel-ns"},
			expectNS:   "otel-ns",
			expectMeta: map[attr.Name]string{"service.namespace": "otel-ns"},
		},
		{
			name:       "No relevant env vars",
			envVars:    map[string]string{"FOO": "BAR"},
			expectMeta: map[attr.Name]string{},
			expectEnv:  map[string]string{},
		},
		{
			name:       "Improper resource attributes, no key - value pairs",
			envVars:    map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "service.namespace,otel-ns"},
			expectMeta: map[attr.Name]string{},
		},
		{
			name:       "Unresolved values in name and namespace",
			envVars:    map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "service.namespace=${test-ns},service.name=$(otel-ns)"},
			expectMeta: map[attr.Name]string{},
		},
		{
			name: "Pre-set metadata is preserved over env resource attributes",
			envVars: map[string]string{
				"OTEL_RESOURCE_ATTRIBUTES": "deployment.environment=prod,custom.attr=from-env",
			},
			expectMeta: map[attr.Name]string{
				"deployment.environment": "staging",
				"custom.attr":            "from-env",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fi := New(Init{})
			if tt.name == "Pre-set metadata is preserved over env resource attributes" {
				fi.SetMetadata(map[attr.Name]string{
					"deployment.environment": "staging",
				})
			}
			fi.ApplyEnvVariables(tt.envVars)
			snap := fi.ServiceAttrs()
			if got := snap.UID.Name; got != tt.expectName {
				t.Errorf("UID.Name = %q, want %q", got, tt.expectName)
			}
			if got := snap.UID.Namespace; got != tt.expectNS {
				t.Errorf("UID.Namespace = %q, want %q", got, tt.expectNS)
			}
			wantEnv := tt.envVars
			if tt.expectEnv != nil {
				wantEnv = tt.expectEnv
			}
			if !reflect.DeepEqual(snap.EnvVars, wantEnv) {
				t.Errorf("EnvVars = %#v, want %#v", snap.EnvVars, wantEnv)
			}
			if !reflect.DeepEqual(snap.Metadata, tt.expectMeta) {
				t.Errorf("Metadata = %#v, want %#v", snap.Metadata, tt.expectMeta)
			}
		})
	}
}

func TestFileInfoKeepsProcessStartTime(t *testing.T) {
	info := New(Init{StartTime: 42})
	if info.StartTime() != 42 {
		t.Fatalf("StartTime() = %d, want 42", info.StartTime())
	}
}

func TestApplyEnvVariablesKeepsOnlyNamedVariables(t *testing.T) {
	envVars := map[string]string{
		"DATABASE_PASSWORD":           "secret",
		"OTEL_EXPORTER_OTLP_HEADERS":  "authorization=Bearer token",
		"AWS_SECRET_ACCESS_KEY":       "secret",
		"otel_service_name":           "lower-case",
		"OTEL_SERVICE_NAME":           "my-service",
		"TMPDIR":                      "/custom/tmp",
		"PYTHONPATH":                  "/app",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318",
	}
	want := map[string]string{
		"OTEL_SERVICE_NAME":           "my-service",
		"TMPDIR":                      "/custom/tmp",
		"PYTHONPATH":                  "/app",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318",
	}

	fi := New(Init{})
	fi.ApplyEnvVariables(envVars)
	if got := fi.ServiceAttrs().EnvVars; !reflect.DeepEqual(got, want) {
		t.Errorf("EnvVars = %#v, want %#v", got, want)
	}
	if got := fi.ServiceAttrs().UID.Name; got != "my-service" {
		t.Errorf("UID.Name = %q, want %q", got, "my-service")
	}
}

func TestApplyEnvVariablesKeepsEveryNamedVariable(t *testing.T) {
	envVars := map[string]string{}
	for name := range keptEnvNames {
		envVars[name] = "v"
	}
	for _, name := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"} {
		envVars[name] = "http://collector:4318"
	}
	envVars["GUNICORN_CMD_ARGS"] = "--name=web"
	envVars["UNNAMED"] = "v"

	fi := New(Init{})
	fi.ApplyEnvVariables(envVars)
	got := fi.ServiceAttrs().EnvVars
	if len(got) != len(keptEnvNames) {
		t.Errorf("kept %d variables, want %d", len(got), len(keptEnvNames))
	}
	if _, ok := got["UNNAMED"]; ok {
		t.Errorf("UNNAMED kept")
	}
}

func TestApplyEnvVariablesWithNoEnvironmentKeepsNone(t *testing.T) {
	fi := New(Init{})
	fi.ApplyEnvVariables(nil)
	if got := fi.ServiceAttrs().EnvVars; got != nil {
		t.Errorf("EnvVars = %#v, want nil", got)
	}
}

func TestApplyEnvVariablesKeepsAnEndpointsSchemeHostAndPortOnly(t *testing.T) {
	tests := map[string]string{
		"https://user:token@collector:4318/v1/traces?key=secret": "https://collector:4318",
		"http://collector:4317":                                  "http://collector:4317",
		"http://[::1]:4318/":                                     "http://[::1]:4318",
	}
	for value, want := range tests {
		fi := New(Init{})
		fi.ApplyEnvVariables(map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": value})
		if got := fi.ServiceAttrs().EnvVars["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]; got != want {
			t.Errorf("%q kept as %q, want %q", value, got, want)
		}
	}
	for _, value := range []string{"collector:4317", "/v1/traces", "http://%zz", ""} {
		fi := New(Init{})
		fi.ApplyEnvVariables(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": value})
		if got, ok := fi.ServiceAttrs().EnvVars["OTEL_EXPORTER_OTLP_ENDPOINT"]; ok {
			t.Errorf("%q kept as %q, want it dropped", value, got)
		}
	}
}

func TestApplyEnvVariablesKeepsOnlyTheGunicornSettingsRead(t *testing.T) {
	fi := New(Init{})
	fi.ApplyEnvVariables(map[string]string{"GUNICORN_CMD_ARGS": "--env DB_PASSWORD=secret -b 0.0.0.0:8000 --chdir /srv/app --name web --keyfile /run/key.pem"})
	if got, want := fi.ServiceAttrs().EnvVars["GUNICORN_CMD_ARGS"], "--chdir='/srv/app' --name='web'"; got != want {
		t.Errorf("GUNICORN_CMD_ARGS = %q, want %q", got, want)
	}

	fi = New(Init{})
	fi.ApplyEnvVariables(map[string]string{"GUNICORN_CMD_ARGS": "--not-an-option secret"})
	if got, ok := fi.ServiceAttrs().EnvVars["GUNICORN_CMD_ARGS"]; ok {
		t.Errorf("an unparsed GUNICORN_CMD_ARGS kept as %q", got)
	}
}
