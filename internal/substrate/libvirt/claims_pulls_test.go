package libvirt

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

func TestTheHostAndMachineCapabilitiesClaimWhatTheirArmDefines(t *testing.T) {
	catalog := catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"), guest("rhel-02"))
	if got, want := (HostCapability{}).Claimed(catalog), []string{"InfraProvider/lab-libvirt", "Network/lab-guests"}; !slices.Equal(got, want) {
		t.Errorf("host claims = %v, want %v", got, want)
	}
	if got := (MachineCapability{}).Claimed(catalog); !slices.Equal(got, []string{"Machine/rhel-01", "Machine/rhel-02"}) {
		t.Errorf("machine claims = %v", got)
	}
	if got := (HostCapability{}).Claimed(catalogOf(controller())); len(got) != 0 {
		t.Errorf("a catalog without a libvirt provider claims %v", got)
	}
}

func TestBothCapabilitiesPrecedeTheController(t *testing.T) {
	definition := reconciliation.BlockDefinition{}
	if !(HostCapability{}).PrecedesController(definition) || !(MachineCapability{}).PrecedesController(definition) {
		t.Error("a libvirt block does not precede the controller")
	}
}

func TestTheEmulatorImageIsTheDigestPinnedImageTheMachinePullsUse(t *testing.T) {
	image := EmulatorImage()
	if image != emulatorImage || image == "" {
		t.Fatalf("EmulatorImage = %q", image)
	}
	pulls := EmulatorPulls(catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"), guest("rhel-02")))
	if len(pulls) != 1 {
		t.Fatalf("two guests of one host pull the image once each host: %+v", pulls)
	}
	if pull := pulls[0]; pull.Origin != image || pull.Pulled == "" || pull.Host != "controller" || pull.Provider != "InfraProvider/lab-libvirt" {
		t.Errorf("pull = %+v", pull)
	}
	if got := EmulatorPulls(catalogOf(controller(), provider())); len(got) != 0 {
		t.Errorf("a provider without machines pulls %+v", got)
	}
}

func TestCapacityAllocationsProjectEachMachineRequestIntoAllocatedQuantities(t *testing.T) {
	environment := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(field("controller", api.MapValue(text("machineRef", "controller")))))
	allocations, err := CapacityAllocations(catalogOf(controller(), environment, provider(), networkConfig(), guest("rhel-01")), testContext)
	if err != nil || len(allocations) != 1 {
		t.Fatalf("allocations = %+v (%v)", allocations, err)
	}
	got := allocations[0]
	if got.Machine != "rhel-01" || got.Provider != "lab-libvirt" || got.Host != "controller" || got.Context != testContext || !got.Local {
		t.Errorf("allocation = %+v", got)
	}
	want := capacity.Quantities{CPU: 4, Memory: 8192 << 20, Disk: 60 << 30}
	if got.Quantities.CPU != want.CPU || got.Quantities.Memory != want.Memory || got.Quantities.Disk < want.Disk {
		t.Errorf("quantities = %+v, want at least %+v", got.Quantities, want)
	}
	if len(got.Disks) == 0 || got.Domain == "" || got.UUID == "" {
		t.Errorf("allocation names no disk, domain or UUID: %+v", got)
	}
}

func TestCapacityAllocationsRefuseACatalogWithoutOneEnvironment(t *testing.T) {
	if allocations, err := CapacityAllocations(labCatalog(), testContext); err == nil || allocations != nil {
		t.Errorf("allocations = %+v (%v)", allocations, err)
	}
}

func TestSecretReportsRefuseABlockWhoseRequestDoesNotDecode(t *testing.T) {
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: []byte("not a request")}}
	if _, err := (HostCapability{}).SecretReferences(block); err == nil {
		t.Error("host references decoded garbage")
	}
	if _, err := (HostCapability{}).SecretClasses(block); err == nil {
		t.Error("host classes decoded garbage")
	}
	if _, err := (MachineCapability{}).SecretReferences(block); err == nil {
		t.Error("machine references decoded garbage")
	}
	if _, err := (MachineCapability{}).SecretClasses(block); err == nil {
		t.Error("machine classes decoded garbage")
	}
}
