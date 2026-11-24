package prerequisites

import (
	"os"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

func TestTheBaselineNamesSshAloneWithoutPodmanOrNmstate(t *testing.T) {
	got, err := BaselineHostRequirements("")
	if err != nil || !slices.Equal(got, []string{"openssh-clients"}) {
		t.Fatal(got, err)
	}
	got[0] = "changed"
	if again, _ := BaselineHostRequirements(""); again[0] != "openssh-clients" {
		t.Fatal("a caller reached the table", again)
	}
}

func TestTheBaselineNamesTheInterpreterPackageOnlyForAnInterpreterToInstall(t *testing.T) {
	got, err := BaselineHostRequirements("3.13")
	if err != nil || !slices.Equal(got, []string{"openssh-clients", "python3.13"}) {
		t.Fatal(got, err)
	}
	for _, minor := range []string{"3", "3.", "3.13.1", "2.7", "3.013", "3.1 ", "x", "3.13\n"} {
		if got, err := BaselineHostRequirements(minor); err == nil {
			t.Errorf("minor %q was admitted as %q", minor, got)
		}
	}
	if abi, ok := PythonRequirementABI(got[1]); !ok || abi != "3.13" {
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
		{"nothing", NativeRequirements{}, []string{}},
		{"container runtime", NativeRequirements{ContainerRuntime: true}, []string{"podman"}},
		{"client", NativeRequirements{LibvirtClient: true}, []string{"libvirt-client"}},
		{"media", NativeRequirements{InstallerMedia: true}, []string{"lorax", "xorriso", "squashfs-tools", "e2fsprogs", "binutils"}},
		{"hypervisor", NativeRequirements{Hypervisor: true}, []string{
			"libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
			"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm-core", "swtpm", "swtpm-tools",
		}},
		{"all", NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true, InstallerMedia: true}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := StageHostRequirements(test.selected)
			if test.want != nil && !slices.Equal(got, test.want) {
				t.Fatal(got)
			}
			if test.name == "all" && (len(got) != 15 || got[0] != "podman" || got[1] != "libvirt-client" || got[14] != "binutils") {
				t.Fatal(got)
			}
		})
	}
}

func TestEveryTableEntryIsAdmittedByTheRequirementGrammar(t *testing.T) {
	baseline, _ := BaselineHostRequirements("3.14")
	all := append(baseline, StageHostRequirements(NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true, InstallerMedia: true, NMState: true, IPUtils: true})...)
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
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.MapValue(), "os", "provided").WithPath(api.MapValue(), "access", "local").WithPath(api.MapValue(), "proxy", "direct")),
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
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.MapValue(), "os", "provided").WithPath(api.MapValue(), "access", "local").WithPath(api.MapValue(), "proxy", "direct")),
		api.NewObject(api.InfraProvider, "hosted", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "libvirt", "machineRef")),
		api.NewObject(api.Network, "lab-guests", api.Value{}, api.MapValue().With("provided", api.BoolValue(false)).WithPath(api.StringValue("198.51.100.0/24"), "ipv4", "cidr").With("libvirt", api.MapValue().With("providerRef", api.StringValue("hosted")).With("bridge", api.StringValue("virbr-lab")).With("hostAddress", api.StringValue("198.51.100.1")))),
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
	if StageNativeOf(controller.Baseline()).ContainerRuntime {
		t.Fatal("baseline selected the container runtime")
	}
}

// hostPackageSpec owns the host-package table, read from this package's
// directory.
const hostPackageSpec = "../../../specs/controller.md"

// hostPackageRows reads the rows of the table under the Host packages heading:
// scope, selecting condition and requirements.
func hostPackageRows(t *testing.T) [][3]string {
	t.Helper()
	data, err := os.ReadFile(hostPackageSpec)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][3]string
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			inSection = line == "## Host packages"
		case inSection && strings.HasPrefix(line, "| Baseline") || inSection && strings.HasPrefix(line, "| Stage"):
			cells := strings.Split(strings.Trim(line, "| "), " | ")
			if len(cells) != 3 {
				t.Fatalf("a host-package row has %d cells: %q", len(cells), line)
			}
			rows = append(rows, [3]string{cells[0], cells[1], cells[2]})
		}
	}
	return rows
}

func requirementsOf(cell string) []string {
	names := []string{}
	for _, name := range strings.Split(cell, ", ") {
		names = append(names, strings.Trim(name, "`"))
	}
	return names
}

// TestHostPackageTableMatchesTheRequirements holds the Host packages table of
// the controller spec to BaselineHostRequirements and StageHostRequirements: the
// baseline rows name the baseline and the interpreter package, and each stage
// row names exactly what its closure selects, in the order the code lists them.
func TestHostPackageTableMatchesTheRequirements(t *testing.T) {
	rows := hostPackageRows(t)
	stages := []NativeRequirements{
		{ContainerRuntime: true}, {LibvirtClient: true}, {Hypervisor: true}, {InstallerMedia: true},
		{ImageMirror: true}, {NMState: true}, {IPUtils: true},
	}
	if len(rows) != 2+len(stages) {
		t.Fatalf("the table holds %d rows, want %d", len(rows), 2+len(stages))
	}
	baseline, err := BaselineHostRequirements("3.12")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "Baseline (setup)" || !slices.Equal(requirementsOf(rows[0][2]), baseline[:1]) {
		t.Errorf("the baseline row is %q, the code names %v", rows[0], baseline[:1])
	}
	if rows[1][0] != "Baseline (setup)" || !slices.Equal(requirementsOf(rows[1][2]), []string{strings.Replace(baseline[1], "3.12", "3.X", 1)}) {
		t.Errorf("the interpreter row is %q, the code names %v", rows[1], baseline[1:])
	}
	var every []string
	for index, selected := range stages {
		row, want := rows[2+index], StageHostRequirements(selected)
		if row[0] != "Stage" || !slices.Equal(requirementsOf(row[2]), want) {
			t.Errorf("the stage row %q names %v, the code names %v", row[1], requirementsOf(row[2]), want)
		}
		every = append(every, want...)
	}
	all := NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true, InstallerMedia: true, ImageMirror: true, NMState: true, IPUtils: true}
	if got := StageHostRequirements(all); !slices.Equal(got, every) {
		t.Errorf("every closure selected names %v, the rows name %v in order", got, every)
	}
}
