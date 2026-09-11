package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Controller-negative tests use explicit documents without serviceSources'
// convenience controller, so an absent or invalid controller stays observable.
func controllerInputs(environment string, objects ...string) desiredstate.Sources {
	content := environment
	for _, object := range objects {
		content += "\n---\n" + object
	}
	return desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content))}}
}

func TestControllerRequiresOneProvidedLocalMachine(t *testing.T) {
	for _, tc := range []struct{ name, environment, machine, extra, code, field string }{
		{"missing controller", strings.Replace(serviceEnvironment, "  controller: {machineRef: service-host}\n", "", 1), serviceHost, "", "api.required", "$.spec.controller"},
		{"missing reference", strings.Replace(serviceEnvironment, "{machineRef: service-host}", "{}", 1), serviceHost, "", "api.required", "$.spec.controller.machineRef"},
		{"missing Machine", serviceEnvironment, "", "", "api.reference", "$.spec.controller.machineRef"},
		{"wrong kind", serviceEnvironment, "", "apiVersion: bootwright.io/v1alpha1\nkind: NTPServer\nmetadata: {name: service-host}\nspec: {management: external, address: time.example.test}\n", "api.reference", "$.spec.controller.machineRef"},
		{"SSH Machine", serviceEnvironment, strings.Replace(serviceHost, "access: {local: true}", "access: {ssh: {auth: {operatorIdentity: {}}}}", 1), "", "api.invariant", "$.spec.controller.machineRef"},
		{"unprovided Machine", serviceEnvironment, strings.Replace(serviceHost, "provided: true", "provided: false", 1), "", "api.invariant", "$.spec.controller.machineRef"},
		{"missing runtime capability", serviceEnvironment, strings.Replace(serviceHost, "  capabilities: [container-runtime]\n", "", 1), "", "api.invariant", "$.spec.controller.machineRef"},
		{"empty capabilities", serviceEnvironment, strings.Replace(serviceHost, "[container-runtime]", "[]", 1), "", "api.invariant", "$.spec.controller.machineRef"},
		{"second local Machine", serviceEnvironment, serviceHost, strings.Replace(serviceHost, "name: service-host", "name: other-local", 1), "api.invariant", "$.spec.controller.machineRef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trustFailure(t, controllerInputs(tc.environment, tc.machine, tc.extra), tc.code, tc.field)
		})
	}
	state, _ := compileAcceptance(t, controllerInputs(serviceEnvironment, serviceHost))
	env := requireObject(t, state.Effective(), api.Environment, "synthetic")
	host := requireObject(t, state.Effective(), api.Machine, "service-host")
	if env.Spec().Get("controller", "machineRef").Text() != host.Name() || !host.Spec().Get("os", "provided").Bool() || !host.Spec().Get("access", "local").Bool() {
		t.Fatal("controller identity was not retained as the declared Machine reference")
	}
}

func TestControllerCannotBeASelectedClusterNode(t *testing.T) {
	t.Run("ContainerCluster", func(t *testing.T) {
		inputs := trustInputs(serviceEnvironment, "")
		for i, file := range inputs.Files {
			if file.Path() == "/synthetic/cluster.yaml" {
				inputs.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(string(file.Bytes()), "machineRef: trust-node", "machineRef: service-host", 1)))
			}
		}
		diagnostic := trustFailure(t, inputs, "api.invariant", "$.spec.controller.machineRef")
		if !strings.Contains(diagnostic.Message, "trust-cluster") {
			t.Fatal("controller conflict omitted its cluster", diagnostic.Message)
		}
	})
	t.Run("StorageCluster", func(t *testing.T) {
		cluster := `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata: {name: storage}
spec:
  type: ceph
  management: managed
  ceph:
    release: 20.2.0
    cephadm:
      bootstrap: {node: member, singleHostDefaults: true}
    topology:
      nodes:
        - name: member
          machineRef: service-host
          roles: [mon, mgr, osd]
          devices: [/dev/sdb, /dev/sdc]
`
		host := strings.Replace(serviceHost, "[container-runtime]", "[container-runtime, ceph-node]", 1)
		diagnostic := trustFailure(t, controllerInputs(serviceEnvironment, host, cluster), "api.invariant", "$.spec.controller.machineRef")
		if !strings.Contains(diagnostic.Message, "storage") {
			t.Fatal("controller conflict omitted its storage cluster", diagnostic.Message)
		}
	})
}

func TestControllerRetentionRespectsResourceSelection(t *testing.T) {
	env := serviceEnvironment + "  containerClusters: [trust-cluster]\n"
	inputs := trustInputs(env, "")
	excludedLocal := strings.Replace(serviceHost, "name: service-host", "name: excluded-local", 1)
	inputs.Files = append(inputs.Files, desiredstate.NewSourceFile("/synthetic/excluded.yaml", []byte(excludedLocal)))
	state, _ := compileAcceptance(t, inputs)
	requireObject(t, state.Effective(), api.Machine, "service-host")
	requireObject(t, state.Effective(), api.Machine, "trust-node")
	if _, found := state.Effective().Find(api.Machine, "excluded-local"); found {
		t.Fatal("an unselected local Machine became a second controller")
	}
	filtered := trustInputs(env+"  resources: [cluster.yaml, dependencies.yaml]\n", "")
	trustFailure(t, filtered, "api.reference", "$.spec.controller.machineRef")
	included := trustInputs(env+"  resources: [cluster.yaml, dependencies.yaml, controller.yaml]\n", "")
	compileAcceptance(t, included)
}

func TestExcludedClusterControllerMembershipDoesNotAffectSelectedGraph(t *testing.T) {
	env := serviceEnvironment + "  containerClusters: [trust-cluster]\n"
	inputs := trustInputs(env, "")
	cluster := strings.Replace(trustClusterPrefix+trustClusterSuffix, "name: trust-cluster", "name: excluded-cluster", 1)
	cluster = strings.Replace(cluster, "machineRef: trust-node", "machineRef: service-host", 1)
	inputs.Files = append(inputs.Files, desiredstate.NewSourceFile("/synthetic/excluded-cluster.yaml", []byte(cluster)))
	state, _ := compileAcceptance(t, inputs)
	if _, found := state.Effective().Find(api.ContainerCluster, "excluded-cluster"); found {
		t.Fatal("excluded cluster was retained by its controller reference")
	}
	requireObject(t, state.Effective(), api.Machine, "service-host")
}

func TestControllerProxyRetainsItsServiceDependencies(t *testing.T) {
	env := serviceEnvironment + "  containerClusters: [trust-cluster]\n"
	proxyHost := strings.Replace(serviceHost, "name: service-host", "name: proxy-host", 1)
	proxyHost = strings.Replace(proxyHost, "access: {local: true}", "access: {ssh: {auth: {operatorIdentity: {}}}}", 1)
	proxy := "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: egress}\nspec:\n  management: managed\n  implementation: squid\n  machineRef: proxy-host\n  endpoints: [{name: controller, addressRef: service}]\n"
	inputs := trustInputs(env, "", proxyHost, proxy)
	for i, file := range inputs.Files {
		if file.Path() == "/synthetic/controller.yaml" {
			host := strings.Replace(serviceHost, "spec:\n", "spec:\n  proxy: {proxyRef: egress}\n", 1)
			inputs.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(host))
		}
	}
	state, _ := compileAcceptance(t, inputs)
	requireObject(t, state.Effective(), api.Machine, "proxy-host")
	requireObject(t, state.Effective(), api.Proxy, "egress")
	controller := requireObject(t, state.Effective(), api.Machine, "service-host")
	if controller.Spec().Get("proxy", "endpointRef").Text() != "controller" {
		t.Fatal("controller proxy lost the endpoint of its retained service")
	}
}

func TestControllerDefaultsAndMachineProxyHaveSeparateOwners(t *testing.T) {
	env := strings.Replace(serviceEnvironment, "  controller: {machineRef: service-host}\n", "", 1) + `  defaults:
    Environment:
      controller: {machineRef: service-host}
    Machine:
      proxy: {proxyRef: egress, noProxy: [second.example.test, first.example.test]}
`
	proxy := "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: egress}\nspec:\n  management: managed\n  implementation: squid\n  machineRef: service-host\n  endpoints: [{name: install, addressRef: service}]\n"
	state, _ := compileAcceptance(t, controllerInputs(env, serviceHost, proxy))
	effectiveEnv := requireObject(t, state.Effective(), api.Environment, "synthetic")
	effectiveHost := requireObject(t, state.Effective(), api.Machine, "service-host")
	choice := effectiveHost.Spec().Get("proxy")
	if choice.Get("proxyRef").Text() != "egress" || choice.Get("endpointRef").Text() != "install" || !slices.Equal(choice.Get("noProxy").Strings(), []string{"second.example.test", "first.example.test"}) {
		t.Fatal("Machine proxy defaults lost service identity, endpoint selection, or bypass order")
	}
	if effectiveEnv.Spec().Get("controller").Len() != 1 || effectiveEnv.Spec().Has("controller", "proxy") || effectiveHost.Spec().Has("os", "install", "proxy") || choice.Has("connection") {
		t.Fatal("controller proxy was copied outside its owning Machine field")
	}
	if requireObject(t, state.Authored(), api.Environment, "synthetic").Spec().Has("controller") || requireObject(t, state.Authored(), api.Machine, "service-host").Spec().Has("proxy") {
		t.Fatal("defaults changed the authored declarations")
	}
	direct := strings.Replace(serviceHost, "spec:\n", "spec:\n  proxy: {direct: {}}\n", 1)
	replaced, _ := compileAcceptance(t, controllerInputs(env, direct, proxy))
	if got := requireObject(t, replaced.Effective(), api.Machine, "service-host").Spec().Get("proxy"); !got.Has("direct") || got.Len() != 1 {
		t.Fatal("explicit direct access did not replace the inherited proxy choice")
	}
	defaulted, _ := compileAcceptance(t, controllerInputs(serviceEnvironment, serviceHost))
	if !requireObject(t, defaulted.Effective(), api.Machine, "service-host").Spec().Get("proxy").Has("direct") {
		t.Fatal("provided controller omitted its direct proxy default")
	}
}

func TestControllerMachineProxyDiagnosticsKeepKindDefaultOrigin(t *testing.T) {
	env := serviceEnvironment + "  defaults:\n    Machine:\n      proxy: {proxyRef: missing-egress}\n"
	diagnostic := trustFailure(t, controllerInputs(env, serviceHost), "api.reference", "$.spec.proxy.proxyRef")
	if diagnostic.Object == nil || diagnostic.Object.Kind != "Machine" || diagnostic.Object.Name != "service-host" || !strings.Contains(diagnostic.Message, "from Environment defaults") || diagnostic.Source == nil {
		t.Fatal("controller proxy diagnostic lost its Machine/default owner", diagnostic)
	}
	lines := strings.Split(env, "\n")
	if diagnostic.Source.Line < 1 || diagnostic.Source.Line > len(lines) || !strings.Contains(lines[diagnostic.Source.Line-1], "missing-egress") {
		t.Fatal("default diagnostic points away from authored reference")
	}
}

func TestRetiredControllerAndInstallProxyExplainMachineField(t *testing.T) {
	for _, tc := range []struct{ name, environment, host, field string }{
		{"controller proxy", strings.Replace(serviceEnvironment, "{machineRef: service-host}", "{machineRef: service-host, proxy: {direct: {}}}", 1), serviceHost, "$.spec.controller.proxy"},
		{"controller proxy default", serviceEnvironment + "  defaults:\n    Environment:\n      controller:\n        proxy: {direct: {}}\n", serviceHost, "$.spec.defaults.Environment.controller.proxy"},
		{"install proxy", serviceEnvironment, strings.Replace(serviceHost, "os: {provided: true}", "os: {provided: true, install: {proxy: {direct: {}}}}", 1), "$.spec.os.install.proxy"},
		{"install proxy default", serviceEnvironment + "  defaults:\n    Machine:\n      os:\n        install:\n          proxy: {direct: {}}\n", serviceHost, "$.spec.defaults.Machine.os.install.proxy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostic := trustFailure(t, controllerInputs(tc.environment, tc.host), "api.field", tc.field)
			guidance := diagnostic.Message + " " + diagnostic.Remediation
			if !strings.Contains(guidance, "Machine") || !strings.Contains(guidance, "proxy") {
				t.Fatal("retired field lacks Machine proxy guidance", diagnostic)
			}
		})
	}
}

func TestControllerAdmissionDoesNotProbeLocalMachine(t *testing.T) {
	// The synthetic controller contact is deliberately unrelated to this runner.
	// Admission must validate the declaration without resolving or probing it.
	host := strings.Replace(serviceHost, "192.0.2.10", "203.0.113.254", 1)
	state, _, err := wireCompiler().Compile(context.Background(), controllerInputs(serviceEnvironment, host))
	if err != nil || state == nil {
		t.Fatal("declarative controller identity required live-host evidence", diagnostics.Of(err))
	}
}
