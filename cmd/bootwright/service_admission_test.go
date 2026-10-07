package main

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func refusedAt(t *testing.T, sources desiredstate.Sources, code, field string) {
	t.Helper()
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if state != nil || err == nil {
		t.Fatalf("validate admitted input it must refuse at %s", field)
	}
	for _, diagnostic := range diagnostics.Of(err) {
		if diagnostic.Field == field && diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("no %s refusal at %s: %#v", code, field, diagnostics.Of(err))
}

func TestServiceShapesNoConsumerCanUseRefuseAtValidate(t *testing.T) {
	t.Run("an empty listener list", func(t *testing.T) {
		object := "apiVersion: bootwright.io/v1alpha1\nkind: ArtifactServer\nmetadata: {name: artifacts}\nspec:\n  management: managed\n  machineRef: service-host\n  bindAddress: 192.0.2.10\n  listeners: []\n"
		refusedAt(t, serviceSources(serviceEnvironment, object), "api.value", "$.spec.listeners")
	})
	t.Run("a tag in kind defaults", func(t *testing.T) {
		env := serviceEnvironment + "  defaults:\n    Proxy:\n      image: {public: 'registry.example.test/squid:6.10'}\n"
		refusedAt(t, serviceSources(env), "api.invariant", "$.spec.defaults.Proxy.image.public")
	})
	t.Run("an HTTP mirror with a port and a query", func(t *testing.T) {
		env := serviceEnvironment + "  downloads: {helmMirror: 'http://mirror.example.test:8080/helm?x=1'}\n"
		refusedAt(t, serviceSources(env), "api.value", "$.spec.downloads.helmMirror")
	})
}
