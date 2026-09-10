package main

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

const serviceEnvironment = "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata: {name: synthetic}\nspec:\n  controller: {machineRef: service-host}\n  domains: {base: example.test}\n"

const serviceHost = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: service-host}
spec:
  capabilities: [container-runtime]
  os: {provided: true}
  access: {local: true}
  network:
    addresses: [{name: service, address: 192.0.2.10}]
`

func serviceSources(environment string, objects ...string) desiredstate.Sources {
	content := environment + "\n---\n" + serviceHost
	for _, object := range objects {
		content += "\n---\n" + object
	}
	return desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content))}}
}

func TestServiceDefaultsPreserveManagementAndAtomicChoices(t *testing.T) {
	t.Run("external skips managed fields", func(t *testing.T) {
		env := serviceEnvironment + `  defaults:
    ArtifactServer:
      management: managed
      machineRef: service-host
      listeners: [{name: web, protocol: http, port: 8080}]
      endpoints: [{name: web, listenerRef: web, addressRef: service}]
      image: {public: example.test/artifacts:1}
`
		object := "apiVersion: bootwright.io/v1alpha1\nkind: ArtifactServer\nmetadata: {name: external}\nspec:\n  management: external\n  endpoints: [{name: media, url: 'https://artifacts.example.test'}]\n"
		state, _ := compileAcceptance(t, serviceSources(env, object))
		spec := requireObject(t, state.Effective(), api.ArtifactServer, "external").Spec()
		for _, field := range []string{"machineRef", "listeners", "tls", "bindAddress", "retention", "image"} {
			if spec.Has(field) {
				t.Errorf("external service inherited managed field %s", field)
			}
		}
	})
	t.Run("managed skips external endpoints", func(t *testing.T) {
		env := serviceEnvironment + "  defaults:\n    ArtifactServer:\n      management: external\n      endpoints: [{name: media, url: 'https://artifacts.example.test'}]\n"
		object := "apiVersion: bootwright.io/v1alpha1\nkind: ArtifactServer\nmetadata: {name: managed}\nspec:\n  management: managed\n  machineRef: service-host\n  listeners: [{name: web, protocol: http, port: 8080}]\n"
		state, _ := compileAcceptance(t, serviceSources(env, object))
		if requireObject(t, state.Effective(), api.ArtifactServer, "managed").Spec().Has("endpoints") {
			t.Fatal("managed service inherited external endpoint representation")
		}
	})
	t.Run("proxy choice replaces endpoint and bypass list", func(t *testing.T) {
		env := serviceEnvironment + `  defaults:
    Machine:
      proxy: {proxyRef: first, endpointRef: wrong-endpoint, noProxy: [wrong.example.test]}
`
		proxy := "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: second}\nspec:\n  management: managed\n  implementation: squid\n  machineRef: service-host\n  endpoints: [{name: provisioning, addressRef: service}]\n"
		host := strings.Replace(serviceHost, "spec:\n", "spec:\n  proxy: {proxyRef: second}\n", 1)
		state, _ := compileAcceptance(t, controllerInputs(env, host, proxy))
		choice := requireObject(t, state.Effective(), api.Machine, "service-host").Spec().Get("proxy")
		if choice.Get("endpointRef").Text() != "provisioning" || choice.Has("noProxy") || choice.Has("connection") {
			t.Fatal("proxy choice mixed references, routing policy, or connection facts")
		}
	})
}

func TestServiceReferenceDiagnosticsPreserveProfileOrigins(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "profile", true: "kind default"}[inherited], func(t *testing.T) {
			sources := expandedExampleSources(t)
			for index, file := range sources.Files {
				body := string(file.Bytes())
				if strings.Contains(body, "kind: MachineInstallProfile\n") && strings.Count(body, "apiVersion:") == 1 {
					if !inherited {
						body = strings.Replace(body, "spec:\n", "spec:\n  proxy: {proxyRef: missing-egress}\n", 1)
					}
				}
				if inherited && strings.Contains(body, "kind: Environment\n") {
					body = strings.Replace(body, "    MachineInstallProfile:\n      proxy:\n        proxyRef: default", "    MachineInstallProfile:\n      proxy:\n        proxyRef: missing-egress", 1)
				}
				sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(body))
			}
			state, _, err := wireCompiler().Compile(context.Background(), sources)
			if state != nil || err == nil {
				t.Fatal("missing inherited proxy was admitted")
			}
			found := false
			for _, diagnostic := range desiredstate.DiagnosticsOf(err) {
				if diagnostic.Object == nil || diagnostic.Object.Kind != string(api.Machine) || diagnostic.Field != "$.spec.proxy.proxyRef" {
					continue
				}
				found = true
				if diagnostic.Source == nil || !strings.Contains(diagnostic.Message, "from MachineInstallProfile") {
					t.Fatalf("Machine diagnostic lost profile origin: %#v", diagnostic)
				}
				if inherited && !strings.Contains(diagnostic.Message, "via Environment defaults") {
					t.Fatal("Machine diagnostic lost the kind-default origin")
				}
				var source []byte
				for _, file := range sources.Files {
					if file.Path() == diagnostic.Source.Path {
						source = file.Bytes()
					}
				}
				lines := strings.Split(string(source), "\n")
				if diagnostic.Source.Line < 1 || diagnostic.Source.Line > len(lines) || !strings.Contains(lines[diagnostic.Source.Line-1], "missing-egress") {
					t.Fatalf("diagnostic points away from the authored reference: %#v", diagnostic.Source)
				}
			}
			if !found {
				t.Fatalf("no Machine diagnostic for inherited missing Proxy: %#v", desiredstate.DiagnosticsOf(err))
			}
		})
	}
}

func TestRetiredInfrastructureInputExplainsReplacement(t *testing.T) {
	for _, object := range []string{
		"apiVersion: bootwright.io/v1alpha1\nkind: InfraComponent\nmetadata: {name: old}\nspec: {}\n",
	} {
		state, _, err := wireCompiler().Compile(context.Background(), serviceSources(serviceEnvironment, object))
		if state != nil || err == nil {
			t.Fatal("retired service kind was accepted")
		}
		diagnostics := desiredstate.DiagnosticsOf(err)
		if len(diagnostics) != 1 || diagnostics[0].Code != "api.kind" || !strings.Contains(diagnostics[0].Message, "ArtifactServer") || !strings.Contains(diagnostics[0].Message, "management") {
			t.Fatalf("retired kind lacks replacement guidance: %#v", diagnostics)
		}
	}
}

func TestUnusedProxyDefaultsRejectLocalContradictionsWithoutResolvingReferences(t *testing.T) {
	for _, owner := range []struct{ kind, prefix, field string }{
		{"ContainerCluster", "      install:\n        ", "install.proxy"},
		{"MachineInstallProfile", "      ", "proxy"},
		{"Machine", "      ", "proxy"},
	} {
		t.Run(owner.kind, func(t *testing.T) {
			for _, choice := range []string{"{direct: {}, noProxy: [example.test]}", "{direct: {}, endpointRef: endpoint}", "{proxyRef: missing-egress}"} {
				env := serviceEnvironment + "  defaults:\n    " + owner.kind + ":\n" + owner.prefix + "proxy: " + choice + "\n"
				host := strings.Replace(serviceHost, "spec:\n", "spec:\n  proxy: {direct: {}}\n", 1)
				inputs := controllerInputs(env, host)
				state, _, err := wireCompiler().Compile(context.Background(), inputs)
				if strings.Contains(choice, "missing-egress") {
					if err != nil || state == nil {
						t.Fatalf("unused partial defaults resolved their references: %v", desiredstate.DiagnosticsOf(err))
					}
					continue
				}
				if err == nil || state != nil {
					t.Fatal("unused contradictory proxy default was admitted")
				}
				found := false
				for _, diagnostic := range desiredstate.DiagnosticsOf(err) {
					if diagnostic.Field == "$.spec.defaults."+owner.kind+"."+owner.field && diagnostic.Code == "api.invariant" {
						found = true
					}
				}
				if !found {
					t.Fatalf("partial default lost its contradiction diagnostic: %#v", desiredstate.DiagnosticsOf(err))
				}
			}
		})
	}
}
