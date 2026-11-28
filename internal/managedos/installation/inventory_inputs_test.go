package installation

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/installfixtures"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestInventoryBootInputsShareQualifiedMediaAndStaticNetwork(t *testing.T) {
	machine := installfixtures.MetalOn("eno1", installfixtures.HardwareNIC("eno1", "52:54:00:01:02:03"))
	image := installfixtures.BootImage()
	catalog := installfixtures.MetalCatalog(machine, installfixtures.MetalNetwork(installfixtures.DeclaredInterface("eno1", "ethernet")))
	imageRef := image.Name()
	if _, err := PinInventoryImage(catalog, imageRef, machine.Identity(), labRecords()); err == nil {
		t.Fatal("an inventory image without a declared checksum was accepted")
	}
	objects := catalog.Objects()
	for index, o := range objects {
		if o.Kind() == api.MachineImage && o.Name() == imageRef {
			objects[index] = image.WithSpec(image.Spec().With("checksum", api.StringValue(bootDigest)))
		}
	}
	catalog = api.NewCatalog(objects)
	records := labRecords()
	media, err := PinInventoryImage(catalog, imageRef, machine.Identity(), records)
	if err != nil || media.SHA256 != bootDigest {
		t.Fatalf("inventory media pin = %+v (%v)", media, err)
	}
	wrongArch := records[bootImageName]
	wrongArch.Release.Arch = "aarch64"
	records[bootImageName] = wrongArch
	if _, err := PinInventoryImage(catalog, imageRef, machine.Identity(), records); err == nil {
		t.Fatal("an inventory image for the wrong architecture was accepted")
	}

	profile, _ := catalog.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(machine))
	selection := profile.Spec().Get("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint")
	publication, placement, server, err := InventoryPublication(catalog, selection, "controller", testContext, machine.Name(), machine.Identity())
	if err != nil || !placement.Local() || publication.CertificateRef == "" || server == "" {
		t.Fatalf("private inventory publication = %+v (%v)", publication, err)
	}
	if _, _, _, err := InventoryPublication(catalog, api.MapValue(), "controller", testContext, machine.Name(), machine.Identity()); err == nil {
		t.Fatal("inventory publication accepted an unresolved listener")
	}
	target, err := substrate.TargetFor(catalog, machine, testContext, "controller")
	if err != nil {
		t.Fatal(err)
	}
	var requirements Requirements
	network, err := InventoryNetworkFor(catalog, machine, target, &requirements)
	if err != nil || network.Interface != "eno1" || network.Address == "" || len(requirements.DNSServers) == 0 {
		t.Fatalf("inventory network = %+v, requirements %+v (%v)", network, requirements, err)
	}
	withoutAddress := machine.WithSpec(machine.Spec().WithPath(api.StringValue("missing"), "network", "installAddressRef"))
	if _, err := InventoryNetworkFor(catalog, withoutAddress, target, &requirements); err == nil {
		t.Fatal("inventory network accepted an unresolved static address")
	}
}
