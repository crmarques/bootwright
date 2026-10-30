package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const maximalHeader = "apiVersion: bootwright.io/v1alpha1\nkind: ClusterAddonBinding\nmetadata:\n  name: %NAME%\n\nspec:\n  clusterRef: %CLUSTER%\n\n  packages:\n"

const maximalCatalog = `        timeout: 40m

        subscription:
          channel: example-channel
          startingCSV: %CSV%
          source: %CATALOG%
          sourceNamespace: openshift-marketplace

          catalogSource:
            name: %CATALOG%
            image: registry.example.test/catalogs/%CATALOG%@sha256:2222222222222222222222222222222222222222222222222222222222222222
            priority: 100
            pollInterval: 15m
            securityContextConfig: legacy
`

func maximalPackage(name, csv string, rest string) string {
	catalog := strings.NewReplacer("%CSV%", csv, "%CATALOG%", name+"-catalog").Replace(maximalCatalog)
	return "    " + name + ":\n" + catalog + rest
}

func maximalBinding(name, cluster string, packages ...string) string {
	return strings.NewReplacer("%NAME%", name, "%CLUSTER%", cluster).Replace(maximalHeader) + strings.Join(packages, "\n")
}

const maximalPools = `
        pools:
          - name: services
            autoAssign: false

            addresses:
              - 198.51.100.128-198.51.100.135

          - name: routed-services
            autoAssign: true

            addresses:
              - 192.0.2.160-192.0.2.167

            l2:
              interface: enp3s0
`

const maximalFDF = `        cephConsumerRef: compact-storage
        defaultStorageClass: true
`

const maximalACM = `
        mce:
          channel: stable-2.11
          startingCSV: multicluster-engine.v2.11.0
          source: acm-catalog
          sourceNamespace: openshift-marketplace

        hub:
          availabilityConfig: Basic
          disableHubSelfManagement: true

          nodeSelector:
            node-role.kubernetes.io/infra: ""

          tolerations:
            - key: node-role.kubernetes.io/infra
              operator: Equal
              value: reserved
              effect: NoSchedule
              tolerationSeconds: 300
`

const maximalGitOps = `
        bootstrap:
          repoURL: https://git.example.test/platform/bootstrap.git
          targetRevision: main
          path: clusters/compact
          destinationNamespace: bootstrap-demo
          credentialsRef: bootstrap-repository

          access:
            clusterRole: bootstrap-writer

          sync:
            automated: true
            prune: true
            selfHeal: true
`

type maximalCase struct {
	example, name, binding string
	packages               []string
	decode                 map[string]func(api.Value) error
}

func decoder[T any](v api.Value) error {
	_, err := addons.Decode[T](v)
	return err
}

func shapeMembers(shape *api.Shape, path string, into *[]string) {
	switch {
	case shape.Type == api.Sequence && shape.Element != nil:
		shapeMembers(shape.Element, path+"[]", into)
	case shape.Type == api.Mapping && len(shape.Fields) > 0:
		for _, f := range shape.Fields {
			*into = append(*into, path+"."+f.Name)
			shapeMembers(f.Shape, path+"."+f.Name, into)
		}
	case shape.Type == api.Mapping && shape.Element != nil:
		*into = append(*into, path+".*")
	}
}

func authoredMembers(shape *api.Shape, value api.Value, path string, into *[]string) {
	switch {
	case shape.Type == api.Sequence && shape.Element != nil:
		for _, item := range value.Items() {
			authoredMembers(shape.Element, item, path+"[]", into)
		}
	case shape.Type == api.Mapping && len(shape.Fields) > 0:
		for _, f := range shape.Fields {
			if v := value.Get(f.Name); v.Present() {
				*into = append(*into, path+"."+f.Name)
				authoredMembers(f.Shape, v, path+"."+f.Name, into)
			}
		}
	case shape.Type == api.Mapping && shape.Element != nil:
		if len(value.Fields()) > 0 {
			*into = append(*into, path+".*")
		}
	}
}

func TestSchemaMaximalPackagesCompileDecodePlanAndCoverEveryMember(t *testing.T) {
	packagesShape, ok := api.Schema(api.ClusterAddonBinding).Field("packages")
	if !ok {
		t.Fatal("the binding schema has no spec.packages")
	}
	nmstate := maximalPackage("nmstate", "kubernetes-nmstate-operator.v0.0.0", "")
	cases := []maximalCase{
		{"lab-sno-nmstate", "nmstate", maximalBinding("nmstate", "sno", nmstate), []string{"nmstate"}, map[string]func(api.Value) error{"nmstate": decoder[api.NMStateConfig]}},
		{"lab-networks", "metallb", maximalBinding("metallb", "networks", maximalPackage("metallb", "metallb-operator.v0.0.0", maximalPools), nmstate), []string{"metallb", "nmstate"}, map[string]func(api.Value) error{"metallb": decoder[api.MetalLBConfig], "nmstate": decoder[api.NMStateConfig]}},
		{"lab-kubevirt-nested", "host-virtualization", maximalBinding("host-virtualization", "host", nmstate, maximalPackage("kubevirt", "kubevirt-hyperconverged-operator.v0.0.0", "")), []string{"kubevirt", "nmstate"}, map[string]func(api.Value) error{"kubevirt": decoder[api.KubeVirtConfig], "nmstate": decoder[api.NMStateConfig]}},
		{"lab-compact-df", "compact-data-foundation", maximalBinding("compact-data-foundation", "compact", maximalPackage("fdf", "odf-operator.v4.20.0", maximalFDF)), []string{"fdf"}, map[string]func(api.Value) error{"fdf": decoder[api.FDFConfig]}},
		{"lab-compact-hub", "compact-hub", maximalBinding("compact-hub", "compact", maximalPackage("acm", "advanced-cluster-management.v2.16.0", maximalACM), maximalPackage("gitops", "openshift-gitops-operator.v1.18.0", maximalGitOps)), []string{"acm", "gitops"}, map[string]func(api.Value) error{"acm": decoder[api.ACMConfig], "gitops": decoder[api.GitOpsConfig]}},
	}
	covered := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.example, func(t *testing.T) {
			state, _, err := wireCompiler().Compile(context.Background(), withBinding(t, tc.example, tc.binding))
			if err != nil {
				t.Fatalf("a schema-maximal configuration is refused: %+v", diagnostics.Of(err))
			}
			authored, found := state.Authored().Find(api.ClusterAddonBinding, tc.name)
			effective, effectiveFound := state.Effective().Find(api.ClusterAddonBinding, tc.name)
			if !found || !effectiveFound {
				t.Fatalf("the compiled catalog has no ClusterAddonBinding/%s", tc.name)
			}
			for _, pkg := range tc.packages {
				shape, _ := packagesShape.Shape.Field(pkg)
				var want, got []string
				shapeMembers(shape.Shape, pkg, &want)
				authoredMembers(shape.Shape, authored.Spec().Get("packages", pkg), pkg, &got)
				for _, member := range want {
					if !slices.Contains(got, member) {
						t.Errorf("the maximal configuration of %s leaves schema member %s unset", pkg, member)
					}
				}
				if err := tc.decode[pkg](effective.Spec().Get("packages", pkg)); err != nil {
					t.Errorf("the Go configuration type of %s does not hold its schema: %v", pkg, err)
				}
				covered[pkg] = true
			}
			if blocks := planAddOns(t, state, tc.example); len(blocks) != len(tc.packages) {
				t.Fatalf("planned %d add-on blocks, want %d", len(blocks), len(tc.packages))
			}
		})
	}
	for _, field := range packagesShape.Shape.Fields {
		if !covered[field.Name] {
			t.Errorf("package %s has no schema-maximal configuration here", field.Name)
		}
	}
}
