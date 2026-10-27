package prerequisites

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

func TestTheBaselineNamesSshAndPodmanAndNoNmstate(t *testing.T) {
	got, err := BaselineHostRequirements("")
	if err != nil || !slices.Equal(got, []string{"openssh-clients", "podman"}) {
		t.Fatal(got, err)
	}
	got[0] = "changed"
	if again, _ := BaselineHostRequirements(""); again[0] != "openssh-clients" {
		t.Fatal("a caller reached the table", again)
	}
}

func TestTheBaselineNamesTheInterpreterPackageOnlyForAnInterpreterToInstall(t *testing.T) {
	got, err := BaselineHostRequirements("3.13")
	if err != nil || !slices.Equal(got, []string{"openssh-clients", "podman", "python3.13"}) {
		t.Fatal(got, err)
	}
	for _, minor := range []string{"3", "3.", "3.13.1", "2.7", "3.013", "3.1 ", "x", "3.13\n"} {
		if got, err := BaselineHostRequirements(minor); err == nil {
			t.Errorf("minor %q was admitted as %q", minor, got)
		}
	}
	if abi, ok := PythonRequirementABI(got[2]); !ok || abi != "3.13" {
		t.Fatalf("the interpreter requirement installs %q", abi)
	}
	for _, other := range []string{"python(abi) = 3.13", "python3", "python3.13t", "python3.13-libs", "podman"} {
		if abi, ok := PythonRequirementABI(other); ok {
			t.Errorf("%q was read as the interpreter of %q", other, abi)
		}
	}
}

func TestTheStageSelectsOnlyWhatTheCapabilitiesNeed(t *testing.T) {
	for _, test := range []struct {
		name     string
		selected NativeRequirements
		want     []string
	}{
		{"nothing", NativeRequirements{ContainerRuntime: true}, []string{}},
		{"client", NativeRequirements{LibvirtClient: true}, []string{"libvirt-client"}},
		{"media", NativeRequirements{InstallerMedia: true}, []string{"lorax", "xorriso"}},
		{"hypervisor", NativeRequirements{Hypervisor: true}, []string{
			"libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
			"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm", "swtpm", "swtpm-tools",
		}},
		{"all", NativeRequirements{LibvirtClient: true, Hypervisor: true, InstallerMedia: true}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := StageHostRequirements(test.selected)
			if test.want != nil && !slices.Equal(got, test.want) {
				t.Fatal(got)
			}
			if test.name == "all" && (len(got) != 11 || got[0] != "libvirt-client" || got[10] != "xorriso") {
				t.Fatal(got)
			}
		})
	}
}

func TestEveryTableEntryIsAdmittedByTheRequirementGrammar(t *testing.T) {
	baseline, _ := BaselineHostRequirements("3.14")
	all := append(baseline, StageHostRequirements(NativeRequirements{LibvirtClient: true, Hypervisor: true, InstallerMedia: true})...)
	for _, requirement := range all {
		if !ValidHostRequirement(requirement) {
			t.Errorf("%q is refused by the grammar", requirement)
		}
	}
}

func TestTheRequirementGrammarAdmitsNamesAndCapabilitiesOnly(t *testing.T) {
	for _, admitted := range []string{"podman", "python(abi) = 3.13", "libvirt-daemon", "python3.13", "config(foo) = 1:2.3-4.fc44", "libstdc++"} {
		if !ValidHostRequirement(admitted) {
			t.Errorf("%q is refused", admitted)
		}
	}
	for _, refused := range []string{"", "-podman", "--allowerasing", "a b", "a;b", "a\nb", "a$(b)", "python(abi) =3.13", "x >= ", "a*", "/usr/bin/x", "a b = 1", "(abi)", "bash >= 1"} {
		if ValidHostRequirement(refused) {
			t.Errorf("%q is admitted", refused)
		}
	}
}

func TestAgentMediaSelectsNMStateFromItsClusterNodes(t *testing.T) {
	objects := []api.Object{
		api.NewObject(api.Environment, "example", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef")),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").WithPath(api.MapValue(), "proxy", "direct")),
		api.NewObject(api.Machine, "node", api.Value{}, api.MapValue()),
		api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.MapValue(), "install", "agent").With("nodes", api.ListValue(api.MapValue().With("machineRef", api.StringValue("node"))))),
	}
	selected, err := controller.Select(api.NewCatalog(objects))
	if err != nil {
		t.Fatal(err)
	}
	if got := StageRequirements(StageNativeOf(selected)); !slices.Equal(got, []string{"nmstate"}) {
		t.Fatalf("agent media packages: %v", got)
	}
	if got := StageRequirements(StageNativeOf(controller.Baseline())); slices.Contains(got, "nmstate") {
		t.Fatal("baseline selected nmstate")
	}
}

func TestOnlyAProbedInstallerVIPSelectsIPUtils(t *testing.T) {
	objects := []api.Object{
		api.NewObject(api.Environment, "example", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef")),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").WithPath(api.MapValue(), "proxy", "direct")),
		api.NewObject(api.InfraProvider, "hosted", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "libvirt", "machineRef").With("networkAttachments", api.ListValue(api.MapValue().With("libvirt", api.MapValue().With("management", api.StringValue("managed")).With("address", api.StringValue("198.51.100.1/24")).With("bridge", api.StringValue("virbr-lab")))))),
	}
	for _, test := range []struct {
		name, address, platform, source string
		want                            bool
	}{
		{"local IPv4 VIP", "198.51.100.10", "baremetal", "installer", true},
		{"off network", "192.0.2.10", "baremetal", "installer", false},
		{"IPv6", "2001:db8::10", "baremetal", "installer", false},
		{"external", "198.51.100.10", "none", "external", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := api.MapValue().WithPath(api.MapValue(), "install", "agent").WithPath(api.MapValue(), "install", "platform", test.platform).With("nodes", api.ListValue(api.MapValue(), api.MapValue(), api.MapValue()))
			for _, slot := range []string{"api", "api-int", "ingress"} {
				spec = spec.WithPath(api.StringValue(test.address), "install", "endpoints", slot, "address").WithPath(api.StringValue(test.source), "install", "endpoints", slot, "source", "type")
			}
			selection, err := controller.Select(api.NewCatalog(append(slices.Clone(objects), api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, spec))))
			if err != nil {
				t.Fatal(err)
			}
			packages := StageRequirements(StageNativeOf(selection))
			if selection.IPUtils() != test.want || slices.Contains(packages, "iputils") != test.want {
				t.Fatal("wrong probe package selection", packages)
			}
		})
	}
	if StageNativeOf(controller.Baseline()).IPUtils {
		t.Fatal("baseline selected IP probing")
	}
}
