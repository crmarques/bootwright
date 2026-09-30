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

const trustClusterPrefix = `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: {name: trust-cluster}
spec:
  distribution:
    release: {version: 4.21.15}
  install:
`

const trustClusterSuffix = `    endpoints:
      api: {source: {type: node}}
      ingress: {source: {type: node}}
  nodes:
    - name: master
      role: master
      machineRef: trust-node
`

const trustDependencies = `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: {name: trust-provider}
spec:
  kubevirt:
    kubeconfigRef: host-credentials
    namespace: workloads
    machineProfiles: [{name: standard, cpu: 4, memoryMiB: 8192, diskGiB: 80}]
  networkAttachments:
    - name: workload
      kubevirt:
        networkRef: {name: workload}
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: trust-node}
spec:
  capabilities: [openshift-node]
  substrate: {providerRef: trust-provider, profileRef: standard}
  os: {provided: false}
  network:
    inline:
      machineNetwork: [{cidr: 192.0.2.0/24}]
      nmstate:
        interfaces: [{name: eth0, type: ethernet}]
    attachmentRef: workload
    addresses: [{name: primary, address: 192.0.2.10/24, interface: eth0}]
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: host-credentials}
spec: {type: opaque}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: openshift-pull-secret}
spec: {type: dockerConfigJson}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: trust-cluster-cluster-admin-ssh-key}
spec: {type: sshKeyPair}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: ca-first}
spec: {type: caBundle}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: ca-second}
spec: {type: caBundle}
`

func trustInputs(environment, trustList string, extras ...string) desiredstate.Sources {
	cluster := trustClusterPrefix
	if trustList != "" {
		cluster += "    additionalTrustBundleRefs: " + trustList + "\n"
	}
	cluster += trustClusterSuffix
	dependencies := trustDependencies
	for _, extra := range extras {
		dependencies += "\n---\n" + extra
	}
	return desiredstate.Sources{
		Roots: []string{"/synthetic"},
		Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(environment)),
			desiredstate.NewSourceFile("/synthetic/cluster.yaml", []byte(cluster)),
			desiredstate.NewSourceFile("/synthetic/dependencies.yaml", []byte(dependencies)),
			desiredstate.NewSourceFile("/synthetic/controller.yaml", []byte(serviceHost)),
		},
	}
}

func trustFailure(t *testing.T, inputs desiredstate.Sources, code, field string) diagnostics.Diagnostic {
	t.Helper()
	state, report, err := wireCompiler().Compile(context.Background(), inputs)
	if err == nil || state != nil || report != nil {
		t.Fatal("invalid trust declaration exposed a compilation result")
	}
	found := diagnostics.Of(err)
	for _, diagnostic := range found {
		if diagnostic.Code == code && diagnostic.Field == field {
			return diagnostic
		}
	}
	t.Fatalf("missing %s at %s: %#v", code, field, found)
	return diagnostics.Diagnostic{}
}

func TestClusterInstallTrustStaysOnItsConsumer(t *testing.T) {
	env := serviceEnvironment
	proxy := "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: egress}\nspec:\n  management: external\n  connection: {httpsProxy: 'http://proxy.example.test:3128'}\n"
	registry := "apiVersion: bootwright.io/v1alpha1\nkind: Registry\nmetadata: {name: images}\nspec:\n  management: external\n  url: registry.example.test\n  trustBundleRef: service-ca\n"
	ca := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: service-ca}\nspec: {type: caBundle}\n"
	inputs := trustInputs(env, "[ca-second, ca-first]", proxy, registry, ca)
	for i, file := range inputs.Files {
		if file.Path() == "/synthetic/controller.yaml" {
			inputs.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(serviceHost, "spec:\n", "spec:\n  proxy: {proxyRef: egress}\n", 1)))
		}
	}
	state, _ := compileAcceptance(t, inputs)
	cluster := requireObject(t, state.Effective(), api.ContainerCluster, "trust-cluster")
	trust := cluster.Spec().Get("install", "additionalTrustBundleRefs")
	if !slices.Equal(trust.Strings(), []string{"ca-second", "ca-first"}) {
		t.Fatal("cluster trust order changed or service trust was implicitly merged")
	}
	authored := requireObject(t, state.Authored(), api.ContainerCluster, "trust-cluster")
	if !authored.Spec().Get("install", "additionalTrustBundleRefs").Equal(trust) {
		t.Fatal("admission replaced authored trust references with materialized trust")
	}
	if requireObject(t, state.Effective(), api.Proxy, "egress").Spec().Has("connection", "trustBundleRef") {
		t.Fatal("cluster trust propagated into the controller's Proxy")
	}
	if got := requireObject(t, state.Effective(), api.Registry, "images").Spec().Get("trustBundleRef").Text(); got != "service-ca" {
		t.Fatal("cluster trust replaced the Registry's own trust choice")
	}
	machine := requireObject(t, state.Effective(), api.Machine, "trust-node")
	if machine.Spec().Has("os", "install", "additionalTrustBundleRefs") {
		t.Fatal("cluster install trust was authored as Machine OS policy")
	}
	environment := requireObject(t, state.Effective(), api.Environment, "synthetic")
	if environment.Spec().Get("controller", "machineRef").Text() != "service-host" || environment.Spec().Has("controller", "proxy") || environment.Spec().Has("trustedCAs") {
		t.Fatal("cluster trust created an Environment trust policy")
	}
	controller := requireObject(t, state.Effective(), api.Machine, "service-host")
	if controller.Spec().Get("proxy", "proxyRef").Text() != "egress" {
		t.Fatal("cluster trust changed the controller Machine's proxy choice")
	}
}

func TestClusterInstallTrustDefaultsReplaceWholeLists(t *testing.T) {
	defaults := serviceEnvironment + "  defaults:\n    ContainerCluster:\n      install:\n        additionalTrustBundleRefs: [ca-second, ca-first]\n"
	for _, tc := range []struct {
		name, environment, authored string
		expected                    []string
	}{
		{"intrinsic empty", serviceEnvironment, "", []string{}},
		{"inherit ordered defaults", defaults, "", []string{"ca-second", "ca-first"}},
		{"replace inherited list", defaults, "[ca-first]", []string{"ca-first"}},
		{"clear inherited list", defaults, "[]", []string{}},
		{"clear unresolved shadowed defaults", strings.Replace(defaults, "[ca-second, ca-first]", "[missing-ca]", 1), "[]", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := compileAcceptance(t, trustInputs(tc.environment, tc.authored))
			actual := requireObject(t, state.Effective(), api.ContainerCluster, "trust-cluster").Spec().Get("install", "additionalTrustBundleRefs")
			if actual.Type() != api.Sequence || !slices.Equal(actual.Strings(), tc.expected) {
				t.Fatalf("unexpected effective trust references: %v", actual.Strings())
			}
			authored := requireObject(t, state.Authored(), api.ContainerCluster, "trust-cluster")
			if authored.Spec().Has("install", "additionalTrustBundleRefs") != (tc.authored != "") {
				t.Fatal("defaults changed the authored field's presence")
			}
		})
	}
}

func TestClusterInstallTrustRejectsInvalidSecretSelections(t *testing.T) {
	for _, tc := range []struct{ name, list, code, field string }{
		{"missing Secret", "[missing-ca]", "api.reference", "$.spec.install.additionalTrustBundleRefs[0]"},
		{"wrong Secret type", "[openshift-pull-secret]", "api.reference", "$.spec.install.additionalTrustBundleRefs[0]"},
		{"duplicate references", "[ca-first, ca-first]", "api.duplicate", "$.spec.install.additionalTrustBundleRefs[1]"},
		{"null list", "null", "api.type", "$.spec.install.additionalTrustBundleRefs"},
		{"null entry", "[null]", "api.type", "$.spec.install.additionalTrustBundleRefs[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trustFailure(t, trustInputs(serviceEnvironment, tc.list), tc.code, tc.field)
		})
	}
}

func TestClusterInstallTrustDiagnosticsKeepDefaultOrigins(t *testing.T) {
	env := serviceEnvironment + "  defaults:\n    ContainerCluster:\n      install:\n        additionalTrustBundleRefs: [missing-ca]\n"
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "authored", true: "inherited"}[inherited], func(t *testing.T) {
			inputs := trustInputs(serviceEnvironment, "[missing-ca]")
			expectedPath := "/synthetic/cluster.yaml"
			if inherited {
				inputs = trustInputs(env, "")
				expectedPath = "/synthetic/environment.yaml"
			}
			diagnostic := trustFailure(t, inputs, "api.reference", "$.spec.install.additionalTrustBundleRefs[0]")
			if diagnostic.Object == nil || diagnostic.Object.Kind != "ContainerCluster" || diagnostic.Object.Name != "trust-cluster" || diagnostic.Source == nil || diagnostic.Source.Path != expectedPath {
				t.Fatalf("trust diagnostic lost recipient or source: %#v", diagnostic)
			}
			if strings.Contains(diagnostic.Message, "from Environment defaults") != inherited {
				t.Fatal("trust diagnostic misidentified inherited intent", diagnostic.Message)
			}
			for _, file := range inputs.Files {
				if file.Path() != expectedPath {
					continue
				}
				lines := strings.Split(string(file.Bytes()), "\n")
				line := diagnostic.Source.Line
				if line < 1 || line > len(lines) || !strings.Contains(lines[line-1], "missing-ca") {
					t.Fatalf("trust diagnostic points away from the reference: %#v", diagnostic.Source)
				}
			}
		})
	}
}

func TestRetiredEnvironmentTrustHasActionableReplacement(t *testing.T) {
	for _, tc := range []struct{ name, extra, field string }{
		{"Environment", "  trustedCAs: {caBundleRefs: [missing-ca]}\n", "$.spec.trustedCAs"},
		{"Environment defaults", "  defaults:\n    Environment:\n      trustedCAs: {caBundleRefs: [missing-ca]}\n", "$.spec.defaults.Environment.trustedCAs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostic := trustFailure(t, serviceSources(serviceEnvironment+tc.extra), "api.field", tc.field)
			guidance := diagnostic.Message + " " + diagnostic.Remediation
			if !strings.Contains(guidance, "retired") || !strings.Contains(guidance, "ContainerCluster") || !strings.Contains(guidance, "additionalTrustBundleRefs") {
				t.Fatal("retired Environment trust lacks consumer-owned replacement guidance", guidance)
			}
		})
	}
}

func TestUnusedClusterInstallTrustDefaultsValidateStructureOnly(t *testing.T) {
	for _, tc := range []struct{ list, code, field string }{
		{"[missing-ca]", "", ""},
		{"[openshift-pull-secret]", "", ""},
		{"[ca-first, ca-first]", "api.duplicate", "$.spec.defaults.ContainerCluster.install.additionalTrustBundleRefs[1]"},
		{"null", "api.type", "$.spec.defaults.ContainerCluster.install.additionalTrustBundleRefs"},
	} {
		t.Run(tc.list, func(t *testing.T) {
			env := serviceEnvironment + "  defaults:\n    ContainerCluster:\n      install:\n        additionalTrustBundleRefs: " + tc.list + "\n"
			secret := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: openshift-pull-secret}\nspec: {type: dockerConfigJson}\n"
			inputs := serviceSources(env, secret)
			if tc.code == "" {
				compileAcceptance(t, inputs)
			} else {
				trustFailure(t, inputs, tc.code, tc.field)
			}
		})
	}
}

// disable-verification is a per-Machine exception, so neither kind default
// that could hand it to many Machines is admitted: the Machine default names
// it directly, and the InfraProvider default would pass it to every Machine
// that provider hosts.
func TestDisableVerificationIsNeverAKindDefault(t *testing.T) {
	for _, tc := range []struct{ name, defaults, field, message string }{
		{"Machine", "    Machine:\n      hardware:\n        management:\n          bmc:\n            virtualMedia:\n              tls: {trust: disable-verification}\n",
			"$.spec.defaults.Machine.hardware.management.bmc.virtualMedia.tls.trust", "disable-verification is a per-Machine exception and never a kind default"},
		{"InfraProvider", "    InfraProvider:\n      baremetal:\n        defaults:\n          bmc:\n            virtualMedia:\n              tls: {trust: disable-verification}\n",
			"$.spec.defaults.InfraProvider.baremetal.defaults.bmc.virtualMedia.tls.trust", "disable-verification is a per-Machine exception and never a provider default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostic := trustFailure(t, serviceSources(serviceEnvironment+"  defaults:\n"+tc.defaults), "api.invariant", tc.field)
			if diagnostic.Message != tc.message {
				t.Fatalf("message = %q", diagnostic.Message)
			}
		})
	}
}

// bmcTrustSources is an Environment whose Machine kind default offers a
// controller trust bundle, a controller Machine that keeps verification, and
// one Machine that turns it off.
func bmcTrustSources() desiredstate.Sources {
	machine := func(name, address, extra, tls string) string {
		return "apiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: " + name + "}\nspec:\n  os: {provided: true}\n" + extra +
			"  network:\n    addresses: [{name: service, address: " + address + "}]\n" +
			"  hardware:\n    management:\n      bmc:\n        address: https://" + name + "-bmc.example.test/redfish/v1/Systems/1\n" +
			"        credentialsRef: bmc\n" + tls
	}
	env := serviceEnvironment + "  defaults:\n    Machine:\n      hardware:\n        management:\n          bmc:\n            tls: {trustBundleRef: bmc-ca}\n"
	content := env + "\n---\n" + machine("service-host", "192.0.2.10", "  capabilities: [container-runtime]\n  access: {local: true}\n", "") +
		"\n---\n" + machine("opted-out", "192.0.2.11", "", "        tls: {verify: false}\n") +
		"\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: bmc-ca}\nspec: {type: caBundle}\n" +
		"\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: bmc}\nspec: {type: usernamePassword}\n"
	return desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content))}}
}

// An Environment kind-default bundle reaches a Machine that keeps verification
// and never one that authors tls.verify: false, so the opt-out compiles and
// its effective controller carries no bundle.
func TestAKindDefaultTrustBundleSkipsAMachineThatOptsOut(t *testing.T) {
	state, _ := compileAcceptance(t, bmcTrustSources())
	tls := requireObject(t, state.Effective(), api.Machine, "opted-out").Spec().Get("hardware", "management", "bmc", "tls")
	if tls.Has("trustBundleRef") || tls.Get("verify").Bool() {
		t.Fatalf("the opted-out Machine's effective controller trust = %v", tls)
	}
	kept := requireObject(t, state.Effective(), api.Machine, "service-host").Spec().Get("hardware", "management", "bmc", "tls")
	if kept.Get("trustBundleRef").Text() != "bmc-ca" || !kept.Get("verify").Bool() {
		t.Fatalf("the verifying Machine's effective controller trust = %v", kept)
	}
}
