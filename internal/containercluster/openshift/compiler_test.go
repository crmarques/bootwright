package openshift_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const clusterYAML = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: example
spec:
  domains:
    base: example.test

  controller:
    machineRef: controller

---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: controller

spec:
  os: {provided: true}
  access: {local: true}
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: host-provider
spec:
  kubevirt:
    kubeconfigRef: host-credentials
    namespace: workloads
    machineProfiles:
      - name: standard
        cpu: 4
        memoryMiB: 8192
        diskGiB: 80
  networkAttachments:
    - name: workload
      kubevirt:
        networkRef:
          name: workload
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: node
spec:
  substrate:
    providerRef: host-provider
    kubevirt:
      profileRef: standard
  os:
    provided: false
  network:
    inline:
      machineNetwork:
        - cidr: 192.0.2.0/24
      nmstate:
        interfaces:
          - name: eth0
            type: ethernet
    attachmentRef: workload
    addresses:
      - name: primary
        address: 192.0.2.10/24
        interface: eth0
---
apiVersion: bootwright.io/v1alpha1
kind: OpenShiftCluster
metadata:
  name: cluster
spec:
  distribution:
    release:
      version: 4.21.15
  install:
    endpoints:
      api:
        source:
          type: node
      ingress:
        source:
          type: node
  nodes:
    - name: master
      role: controlPlane
      machineRef: node
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: host-credentials
spec:
  type: opaque
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: openshift-pull-secret
spec:
  type: dockerConfigJson
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: cluster-cluster-admin-ssh-key
spec:
  type: sshKeyPair
`

func compiler() compilation.Compiler {
	return compilerWithSelection(nil)
}

func compilerWithSelection(selection compilation.SelectGraph) compilation.Compiler {
	return compilation.NewCompiler(yamlstream.Parser{}, selection,
		compilation.Rules{Normalize: environment.Normalize, Validate: testEnvironmentValidate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, Validate: secrets.Validate},
		compilation.Rules{Normalize: substrate.Normalize, ValidateAuthored: substrate.ValidateAuthored, Validate: substrate.Validate},
		compilation.Rules{Normalize: machine.Normalize, ValidateAuthored: machine.ValidateAuthored, Validate: machine.Validate},
		compilation.Rules{Normalize: managedos.Normalize, ValidateAuthored: managedos.ValidateAuthored, Validate: managedos.Validate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, Validate: infrastructureservices.Validate},
		compilation.Rules{Normalize: openshift.Normalize, ValidateAuthored: openshift.ValidateAuthored, Validate: openshift.Validate},
	)
}

func TestExcludedMachineServiceReferencesDoNotFailSelectedGraph(t *testing.T) {
	selection := func(catalog api.Catalog) compilation.Selection {
		selected := environment.Select(catalog, nil, testClusterMembers)
		result := compilation.Selection{Catalog: selected.Catalog, ExcludedContainerClusters: selected.ExcludedContainerClusters, ExcludedStorageClusters: selected.ExcludedStorageClusters, Excluded: selected.Excluded}
		for _, problem := range selected.Problems {
			result.Problems = append(result.Problems, compilation.ObjectIssue{Object: problem.Object, Issue: problem.Issue, Target: problem.Target})
		}
		return result
	}
	c := compilerWithSelection(selection)
	for _, tc := range []struct {
		name, choice, field string
	}{
		{"proxy", "  proxy: {proxyRef: missing-egress}", "$.spec.proxy.proxyRef"},
		{"ntp", "  os:\n    provided: false\n    installProfileRef: deferred-profile\n    install:\n      ntp: [{serverRef: missing-clock}]", "$.spec.os.install.ntp[0].serverRef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deferred := `
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: deferred
spec:
` + tc.choice + "\n"
			if tc.name == "proxy" {
				deferred += "  os: {provided: false, installProfileRef: deferred-profile}\n"
			}
			deferred += "---\napiVersion: bootwright.io/v1alpha1\nkind: OpenShiftCluster\nmetadata:\n  name: deferred-cluster\nspec:\n  nodes: [{name: deferred, role: controlPlane, machineRef: deferred}]\n"
			content := strings.Replace(clusterYAML, "spec:\n  domains:", "spec:\n  containerClusters: [cluster]\n  domains:", 1) + deferred
			state, _, err := c.Compile(context.Background(), sources(content))
			if err != nil {
				t.Fatal("excluded Machine references failed selected graph", diagnostics.Of(err))
			}
			if _, retained := state.Effective().Find(api.Machine, "deferred"); retained {
				t.Fatal("a node of an excluded cluster was retained by cluster selection")
			}
			_, _, err = c.Compile(context.Background(), sources(clusterYAML+deferred))
			found := false
			for _, diagnostic := range diagnostics.Of(err) {
				if diagnostic.Object != nil && diagnostic.Object.Kind == string(api.Machine) && diagnostic.Object.Name == "deferred" && diagnostic.Field == tc.field {
					found = true
				}
			}
			if !found {
				t.Fatal("retained Machine did not validate its service reference", diagnostics.Of(err))
			}
		})
	}
}
func sources(content string) desiredstate.Sources {
	return desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content))}, Roots: []string{"/synthetic"}}
}

func TestCompleteContainerGraphCompilation(t *testing.T) {
	state, _, err := compiler().Compile(context.Background(), sources(clusterYAML))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	cluster, ok := state.Effective().Find(api.OpenShiftCluster, "cluster")
	if !ok {
		t.Fatal("cluster missing")
	}
	if openshift.Platform(cluster.Spec()) != "none" || cluster.Spec().Get("install", "endpoints", "api", "address").Text() != "192.0.2.10" {
		t.Fatal("effective container fields missing")
	}
	authored, _ := state.Authored().Find(api.OpenShiftCluster, "cluster")
	if authored.Spec().Has("install", "endpoints", "api", "address") || authored.Spec().Has("install", "nodeSSH") {
		t.Fatal("authored container changed")
	}
	if cluster.Spec().Get("networking", "clusterNetwork").Items()[0].Get("hostPrefix").Text() != "23" {
		t.Fatal("schema-dependent network default missing")
	}
}

func TestContainerSchemaAndAuthoredFailures(t *testing.T) {
	cases := map[string]string{
		"release without patch":    strings.Replace(clusterYAML, "version: 4.21.15", `version: "4.21"`, 1),
		"retired pool":             strings.Replace(clusterYAML, "  nodes:\n", "  controlPlane: {}\n  nodes:\n", 1),
		"nonmatching platform":     strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    platform:\n      none: {}\n      vsphere: {}\n    endpoints:", 1),
		"derived address as input": strings.Replace(clusterYAML, "      api:\n        source:", "      api:\n        address: 192.0.2.10\n        source:", 1),
		"mixed node SSH":           strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    nodeSSH:\n      keyPairRef: cluster-cluster-admin-ssh-key\n      publicKeyRef: cluster-cluster-admin-ssh-key\n    endpoints:", 1),
		"wrong pull Secret":        strings.Replace(clusterYAML, "  type: dockerConfigJson", "  type: opaque", 1),
		"unpinned image":           strings.Replace(clusterYAML, "      version: 4.21.15", "      image: quay.io/example/release:latest", 1),
		"PCR fields":               strings.Replace(clusterYAML, "  nodes:\n", "  security:\n    diskEncryption:\n      unlock:\n        tpm2:\n          pcrIds: [7]\n  nodes:\n", 1),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			state, report, err := compiler().Compile(context.Background(), sources(content))
			if err == nil || state != nil || report != nil {
				t.Fatal("invalid compilation succeeded")
			}
		})
	}
}

func TestReleaseWithoutPatchIsRefusedByTheReleaseGrammar(t *testing.T) {
	content := strings.Replace(clusterYAML, "version: 4.21.15", `version: "4.21"`, 1)
	_, _, err := compiler().Compile(context.Background(), sources(content))
	found := slices.DeleteFunc(diagnostics.Of(err), func(d diagnostics.Diagnostic) bool { return d.Severity == "warning" })
	if len(found) != 1 || found[0].Code != "api.invariant" || found[0].Field != "$.spec.distribution.release.version" {
		t.Fatalf("refusal = %#v", found)
	}
}

func TestSplitPublicSSHAndNativeExternalPlatform(t *testing.T) {
	content := strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    nodeSSH:\n      publicKeyRef: cluster-cluster-admin-ssh-key\n    platform:\n      external:\n        name: custom\n        settings:\n          enabled: true\n          count: 2\n          ratio: 0.25\n    endpoints:", 1)
	state, _, err := compiler().Compile(context.Background(), sources(content))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	cluster, _ := state.Effective().Find(api.OpenShiftCluster, "cluster")
	if cluster.Spec().Get("install", "nodeSSH").Has("keyPairRef") || cluster.Spec().Get("install", "nodeSSH").Has("privateKeyRef") {
		t.Fatal("public-only SSH acquired private material")
	}
	if cluster.Spec().Get("install", "platform", "external", "settings", "ratio").Type() != api.Number {
		t.Fatal("native external number not retained")
	}
}

func TestExplicitEndpointAndPlatformChoicesSuppressDefaults(t *testing.T) {
	defaults := `    base: example.test
  defaults:
    OpenShiftCluster:
      install:
        platform:
          baremetal:
            provisioningNetwork: disabled
        endpoints:
          api:
            address: 192.0.2.2
            source:
              type: external
          ingress:
            source:
              type: loadBalancer
              loadBalancerRef: unused-lb
              bindAddressRef: unused
`
	// The inherited endpoint fragment is partial; the explicit source controls
	// which of its conditional fields can reach this recipient.
	content := strings.Replace(clusterYAML, "    base: example.test\n", defaults, 1)
	content = strings.Replace(content, "  install:\n    endpoints:", "  install:\n    platform:\n      none: {}\n    endpoints:", 1)
	state, _, err := compiler().Compile(context.Background(), sources(content))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	cluster, _ := state.Effective().Find(api.OpenShiftCluster, "cluster")
	if cluster.Spec().Get("install", "platform").Has("baremetal") {
		t.Fatal("inactive platform defaults inherited")
	}
	endpoint := cluster.Spec().Get("install", "endpoints", "api")
	if endpoint.Get("address").Text() != "192.0.2.10" || endpoint.Get("source").Has("loadBalancerRef") || endpoint.Get("source").Has("bindAddressRef") {
		t.Fatal("inactive endpoint defaults inherited")
	}
	if cluster.Spec().Get("install", "endpoints", "ingress", "source").Has("loadBalancerRef") {
		t.Fatal("inactive component selection inherited")
	}
}
