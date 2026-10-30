package controller_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/controller"
)

func TestTargetToolLimitCountsDistinctRequirements(t *testing.T) {
	for _, test := range []struct {
		name     string
		clusters int
		distinct bool
	}{
		{"repeated consumers", 43, false},
		{"excessive distinct releases", 64, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := selectionObjects()
			for index := range test.clusters {
				version := "4.21.15"
				if test.distinct {
					version = fmt.Sprintf("4.21.%d", index)
				}
				objects = append(objects, api.NewObject(api.OpenShiftCluster, fmt.Sprintf("cluster-%d", index), api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue(version), "distribution", "release", "version")))
			}
			tools, err := controller.SelectTools(api.NewCatalog(objects))
			if test.distinct {
				if err == nil {
					t.Fatal("more than 128 distinct tool requirements accepted")
				}
				return
			}
			want := []controller.ToolRequest{{Kind: "helm", Version: "latest"}, {Kind: "openshift-clients", Version: "4.21.15", Compatibility: "openshift"}, {Kind: "openshift-install", Version: "4.21.15", Compatibility: "openshift"}}
			if err != nil || !reflect.DeepEqual(tools, want) {
				t.Fatal("repeated consumers changed the required tool closure", tools, err)
			}
		})
	}
}

func TestTargetToolsFollowExactSelectedReleaseAndGenericLatest(t *testing.T) {
	objects := selectionObjects()
	objects = append(objects, api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version")))
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || len(tools) != 3 {
		t.Fatal(tools, err)
	}
	for _, tool := range tools {
		if tool.Kind == "helm" {
			if tool.Version != "latest" {
				t.Fatal(tool)
			}
		} else if tool.Version != "4.21.15" || tool.Compatibility != "openshift" {
			t.Fatal(tool)
		}
	}
	slices.Reverse(objects)
	reordered, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || !reflect.DeepEqual(tools, reordered) {
		t.Fatal("tool order depends on object order", err)
	}
}

func TestTargetToolsRefuseImageVersionAssumptions(t *testing.T) {
	objects := selectionObjects()
	cluster := api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("quay.io/example/release@sha256:abc"), "distribution", "release", "image"))
	if _, err := controller.SelectTools(api.NewCatalog(append(objects, cluster))); err == nil {
		t.Fatal("unknown payload metadata selected tools")
	}
}

func TestUnusedProviderDoesNotRequestTools(t *testing.T) {
	objects := append(selectionObjects(), api.NewObject(api.InfraProvider, "unused", api.Value{}, api.MapValue().With("vsphere", api.MapValue())))
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || len(tools) != 0 {
		t.Fatal(tools, err)
	}
}

func TestNativeLibvirtAndRemoteCephPreparationDoNotRequestUnrelatedDownloads(t *testing.T) {
	objects := append(selectionObjects(),
		api.NewObject(api.InfraProvider, "hypervisor", api.Value{}, api.MapValue().With("libvirt", api.MapValue())),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor"), "substrate", "providerRef")),
		api.NewObject(api.CephCluster, "storage", api.Value{}, api.MapValue().With("management", api.StringValue("managed")).WithPath(api.StringValue("19.2.1"), "release")),
	)
	// virsh and OpenSSH belong to the native package closure. cephadm executes
	// on its selected bootstrap host through Ansible's SSH connection.
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || len(tools) != 0 {
		t.Fatal("native or remote requirements became generic downloads", tools, err)
	}
}

func TestReferencedVSphereRequestsOnlyItsGenericClient(t *testing.T) {
	objects := append(selectionObjects(),
		api.NewObject(api.InfraProvider, "virtualization", api.Value{}, api.MapValue().With("vsphere", api.MapValue())),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("virtualization"), "substrate", "providerRef")),
	)
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || !reflect.DeepEqual(tools, []controller.ToolRequest{{Kind: "govc", Version: "latest"}}) {
		t.Fatal(tools, err)
	}
}

func TestVirtualizationProfileRequestsUpstreamLatestIndependentlyOfCSV(t *testing.T) {
	objects := append(selectionObjects(),
		api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.20.8"), "distribution", "release", "version")),
		api.NewObject(api.ClusterAddon, "virtualization", api.Value{}, api.MapValue().With("provides", api.StringList("kubevirt")).WithPath(api.StringValue("kubevirt-hyperconverged-operator.v4.20.2"), "olm", "subscription", "startingCSV")),
		api.NewObject(api.ClusterAddonProfile, "platform", api.Value{}, api.MapValue().With("addonRefs", api.StringList("virtualization"))),
		api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, api.MapValue().With("clusterRef", api.StringValue("cluster")).With("profileRefs", api.StringList("platform"))),
	)
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(tools, func(tool controller.ToolRequest) bool { return tool.Kind == "virtctl" })
	if index < 0 || tools[index].Compatibility != "kubevirt" || tools[index].Version != "latest" {
		t.Fatal("virtualization did not request the latest upstream client", tools)
	}
}

func TestKubeVirtRequirementNeedsNoVendorCSVOrClusterObservation(t *testing.T) {
	for _, access := range []string{"hostClusterRef", "kubeconfigRef"} {
		objects := append(selectionObjects(),
			api.NewObject(api.InfraProvider, "virtualization", api.Value{}, api.MapValue().WithPath(api.StringValue("host"), "kubevirt", access)),
			api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("virtualization"), "substrate", "providerRef")),
		)
		tools, err := controller.SelectTools(api.NewCatalog(objects))
		if err != nil || !reflect.DeepEqual(tools, []controller.ToolRequest{{Kind: "virtctl", Version: "latest", Compatibility: "kubevirt"}}) {
			t.Fatal(access, tools, err)
		}
	}
	objects := append(selectionObjects(),
		api.NewObject(api.ClusterAddon, "virtualization", api.Value{}, api.MapValue().With("provides", api.StringList("kubevirt")).With("manifestSet", api.MapValue())),
		api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, api.MapValue().With("clusterRef", api.StringValue("cluster")).With("addonRefs", api.StringList("virtualization"))),
	)
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || len(tools) != 1 || tools[0].Kind != "virtctl" || tools[0].Version != "latest" {
		t.Fatal("selected manifest add-on omitted its client", tools, err)
	}
}

func TestEnvironmentDependencyVersionsOverrideOnlyRequiredVariableClients(t *testing.T) {
	objects := selectionObjects()
	for index, object := range objects {
		if object.Kind() == api.Environment {
			objects[index] = api.NewObject(api.Environment, object.Name(), api.Value{}, object.Spec().With("dependencyVersions", api.MapValue().With("helm", api.StringValue("4.2.3")).With("govc", api.StringValue("v0.54.0")).With("virtctl", api.StringValue("1.8.2"))))
		}
	}
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || len(tools) != 0 {
		t.Fatal("version overrides installed unrelated clients", tools, err)
	}
	objects = append(objects,
		api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version")),
		api.NewObject(api.InfraProvider, "vsphere", api.Value{}, api.MapValue().With("vsphere", api.MapValue())),
		api.NewObject(api.InfraProvider, "kubevirt", api.Value{}, api.MapValue().With("kubevirt", api.MapValue())),
		api.NewObject(api.Machine, "one", api.Value{}, api.MapValue().WithPath(api.StringValue("vsphere"), "substrate", "providerRef")),
		api.NewObject(api.Machine, "two", api.Value{}, api.MapValue().WithPath(api.StringValue("kubevirt"), "substrate", "providerRef")),
	)
	tools, err = controller.SelectTools(api.NewCatalog(objects))
	if err != nil || len(tools) != 5 {
		t.Fatal(tools, err)
	}
	wanted := map[string]string{"helm": "v4.2.3", "govc": "v0.54.0", "virtctl": "v1.8.2", "openshift-clients": "4.21.15", "openshift-install": "4.21.15"}
	for _, tool := range tools {
		if tool.Version != wanted[tool.Kind] {
			t.Fatal("override changed a coupled target release or lost a client version", tool)
		}
	}
}

func TestTargetCLIVersionOverridesRejectMalformedSelections(t *testing.T) {
	withVirtctl := func(value api.Value) []api.Object {
		objects := selectionObjects()
		for index, object := range objects {
			if object.Kind() == api.Environment {
				objects[index] = api.NewObject(api.Environment, object.Name(), api.Value{}, object.Spec().WithPath(value, "dependencyVersions", "virtctl"))
			}
		}
		return objects
	}
	for _, value := range []api.Value{api.StringValue(""), api.StringValue("v"), api.StringValue("vlatest"), api.StringValue("1.2 3"), api.StringValue("../1.2.3"), api.StringValue(strings.Repeat("1", 96)), api.BoolValue(true)} {
		if _, err := controller.SelectTools(api.NewCatalog(withVirtctl(value))); err == nil {
			t.Fatal("malformed CLI version override accepted", value)
		}
	}
	for _, value := range []string{"latest", "v1.2.3", "v1.2.3-rc.1", "1.02.3", "1.2.3+build", "v1.2", strings.Repeat("1", 95), "v" + strings.Repeat("1", 95)} {
		if _, err := controller.SelectTools(api.NewCatalog(withVirtctl(api.StringValue(value)))); err != nil {
			t.Fatal("exact publisher release tag refused", value, err)
		}
	}
}

func TestTargetCLIVersionsCoexistWithOtherDependencyOverrides(t *testing.T) {
	versions := api.MapValue().With("helm", api.StringValue("latest"))
	for _, dependency := range []string{"libvirt", "govc", "virtctl"} {
		versions = versions.With(dependency, api.StringValue("latest"))
	}
	objects := selectionObjects()
	for index, object := range objects {
		if object.Kind() == api.Environment {
			objects[index] = api.NewObject(api.Environment, object.Name(), api.Value{}, object.Spec().With("dependencyVersions", versions))
		}
	}
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil || len(tools) != 0 {
		t.Fatal("unselected dependency override selected a target CLI", tools, err)
	}
	for index, object := range objects {
		if object.Kind() == api.Environment {
			objects[index] = api.NewObject(api.Environment, object.Name(), api.Value{}, object.Spec().WithPath(api.StringValue("latest"), "dependencyVersions", "unknown"))
		}
	}
	if _, err := controller.SelectTools(api.NewCatalog(objects)); err == nil {
		t.Fatal("unknown dependency override accepted")
	}
}

// A pinned release image is the payload; the declared release still selects the
// matching clients, so it must not block the rest of controller setup.
func TestTargetToolsFollowDeclaredReleaseBesideAPinnedImage(t *testing.T) {
	objects := selectionObjects()
	cluster := api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().
		WithPath(api.StringValue("openshift"), "distribution", "type").
		WithPath(api.StringValue("4.21.15"), "distribution", "release", "version").
		WithPath(api.StringValue("quay.io/example/release@sha256:abc"), "distribution", "release", "image"))
	tools, err := controller.SelectTools(api.NewCatalog(append(objects, cluster)))
	if err != nil {
		t.Fatalf("a declared release beside a pinned image was refused: %v", err)
	}
	found := map[string]string{}
	for _, tool := range tools {
		found[tool.Kind] = tool.Version
	}
	if found["openshift-clients"] != "4.21.15" || found["openshift-install"] != "4.21.15" {
		t.Fatalf("clients did not follow the declared release: %#v", tools)
	}
}

func TestTargetToolsUseTheClusterReleaseGrammar(t *testing.T) {
	for _, version := range []string{"4.21", "4.22.0-rc.1"} {
		objects := []api.Object{api.NewObject(api.Environment, "example", api.Value{}, api.MapValue()), api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue(version), "distribution", "release", "version"))}
		_, err := controller.SelectTools(api.NewCatalog(objects))
		if (err == nil) != (version == "4.22.0-rc.1") {
			t.Fatalf("release %s: %v", version, err)
		}
	}
}

func TestTargetToolsReadPackageCapabilityFromInstanceSelection(t *testing.T) {
	registry, err := addons.NewRegistry([]addons.Package{{Name: "fixture", Summary: "fixture package", Provides: []string{"kubevirt"}, Budget: time.Minute, Render: func(addons.Instance) (addons.Rendered, error) { return addons.Rendered{}, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	objects := selectionObjects()
	objects = append(objects, api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, api.MapValue().With("clusterRef", api.StringValue("cluster")).WithPath(api.MapValue(), "packages", "fixture")))
	tools, err := controller.SelectToolsWithRegistry(api.NewCatalog(objects), registry)
	if err != nil || !slices.ContainsFunc(tools, func(tool controller.ToolRequest) bool {
		return tool.Kind == "virtctl" && tool.Compatibility == "kubevirt"
	}) {
		t.Fatalf("package tools: %+v %v", tools, err)
	}
}

func TestFIPSAndOrdinaryClustersKeepDistinctInstallerTools(t *testing.T) {
	objects := selectionObjects()
	for _, fips := range []bool{false, true} {
		spec := api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.10"), "distribution", "release", "version").WithPath(api.BoolValue(fips), "security", "fips", "enabled")
		objects = append(objects, api.NewObject(api.OpenShiftCluster, fmt.Sprintf("cluster-%t", fips), api.Value{}, spec))
	}
	tools, err := controller.SelectTools(api.NewCatalog(objects))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"openshift-install", "openshift-install-fips"} {
		if !slices.ContainsFunc(tools, func(r controller.ToolRequest) bool {
			return r.Kind == kind && r.Version == "4.21.10" && r.Compatibility == "openshift"
		}) {
			t.Fatalf("missing %s in %v", kind, tools)
		}
	}
}
