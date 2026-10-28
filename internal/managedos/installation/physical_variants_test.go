package installation

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

func physicalVariantCatalog(profile api.Object, hostType string) api.Catalog {
	return physicalVariantCatalogOn(profile, hostType, api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4")))
}

func physicalVariantCatalogOn(profile api.Object, hostType string, rootDevice api.Value) api.Catalog {
	source := api.MapValue(field("generated", api.MapValue(text("keyType", hostType))))
	objects := labCatalog(metalProvider(), server(rootDevice), sshKeySecret(source), profile, rhelEntitlement(), fleetSecret(api.MapValue(field("generated", api.MapValue(text("keyType", "ecdsaP256")))))).Objects()
	return api.NewCatalog(slices.DeleteFunc(objects, func(object api.Object) bool { return object.Kind() == api.Machine && object.Name() == "rhel-01" }))
}

func TestPhysicalHostVariantsKeepPrivateTargetAndRootProofs(t *testing.T) {
	for _, profile := range []api.Object{fipsProfile(), encryptingProfile(), registeringProfile()} {
		catalog := physicalVariantCatalog(profile, "ecdsaP256")
		if refused := Refusals(catalog); len(refused) != 0 {
			t.Fatalf("physical variant refused: %+v", refused)
		}
		request, _ := onlyRequest(t, catalog)
		if !request.Target.Physical || request.Target.Channel != substrate.ChannelDeliveredKey || request.Private == nil || request.Image != nil || request.Target.HostKeyRef != "metal-01-host-key" || request.Target.HostKeyType != "ecdsaP256" {
			t.Fatal("physical target/private identity lost")
		}
		if !strings.Contains(request.Kickstart, "ignoredisk --only-use=disk/by-id/wwn-0x5000c500a1b2c3d4") || !strings.Contains(request.Kickstart, "clearpart --all --initlabel --drives=disk/by-id/wwn-0x5000c500a1b2c3d4") || !strings.Contains(request.Kickstart, "52:54:00:9a:1b:01") || !strings.Contains(request.Kickstart, "is not a whole disk") {
			t.Fatal("physical installer proof lost")
		}
		if profile.Spec().Has("subscription") && request.Registration == nil {
			t.Fatal("registration ownership omitted")
		}
		if diskEncryption(profile).Present() && (request.DiskEncryption == nil || !strings.Contains(request.Kickstart, "/dev/tpmrm0")) {
			t.Fatal("TPM/recovery prerequisite omitted")
		}
	}
}

func TestPhysicalFIPSRefusesDeclaredEd25519HostKey(t *testing.T) {
	refused := Refusals(physicalVariantCatalog(fipsProfile(), "ed25519"))
	if len(refused) != 1 || !strings.Contains(refused[0].Reason, "metal-01-host-key") || !strings.Contains(refused[0].Reason, "Ed25519") {
		t.Fatalf("host key refused incorrectly: %+v", refused)
	}
}

func TestPhysicalFIPSRefusesBoundIncompatibleHostKeyBeforeRunner(t *testing.T) {
	for _, keyType := range []string{"ed25519", "rsa"} {
		call, request := execution(t, "digest")
		request.InstallerArguments = append(request.InstallerArguments, "fips=1")
		request.Target.Channel = substrate.ChannelDeliveredKey
		request.Target.HostKeyType = keyType
		request.Target.HostKeyRef = "metal-01-host-key"
		line := "ssh-ed25519 AAAAHOST"
		if keyType == "rsa" {
			line = rsaPublicLine(1024)
		}
		call.Material["bootwright-machine-key"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte("ecdsa-sha2-nistp256 AAAAPUBLIC"), secrets.PrivateKeyPart: []byte("PRIVATE")})
		call.Material["metal-01-host-key"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte(line)})
		marker, _ := MarkerFor(request, "digest")
		runner := &fakeRunner{}
		_, err := New(runner).run(context.Background(), call, "apply", request, marker, "")
		reported := diagnostics.Of(err)
		if len(runner.requests) != 0 || len(reported) != 1 || !strings.Contains(reported[0].Message, "Secret/metal-01-host-key") {
			t.Fatalf("incompatible delivered key reached %d runner calls: %v", len(runner.requests), reported)
		}
	}
}

func TestPhysicalVariantRequestRetainsApplyDataLossAuthorization(t *testing.T) {
	catalog := physicalVariantCatalog(encryptingProfile(), "ecdsaP256")
	input := planInput(reconciliation.Apply)
	input.State = compilation.NewState(catalog, catalog, nil)
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatal("physical plan", err)
	}
	if !slices.Contains(plan.Definitions[0].Consumes, reconciliation.AuthorizationDataLoss) {
		t.Fatal("physical installation lost data-loss authorization")
	}
	removal, err := New(nil).Removal(context.Background(), reconciliation.Block{BlockDefinition: plan.Definitions[0]})
	if err != nil || len(removal.Consumes) != 0 {
		t.Fatal("retaining removal acquired erase authority", err)
	}
}

func TestPhysicalHostVariantsRequestGolden(t *testing.T) {
	profile := registeringProfile()
	profile = profile.WithSpec(profile.Spec().WithPath(encryptionSecurity(true), "customizations", "security"))
	request, _ := onlyRequest(t, physicalVariantCatalogOn(profile, "ecdsaP256", api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"),
		text("wwn", "0X5000C500A1B2C3D4"), text("serialNumber", "ZC1A2B3C"))))
	if !request.fips() || request.Registration == nil || request.DiskEncryption == nil {
		t.Fatal("combined physical variant lost a requested arm")
	}
	if hardware := request.Target.Hardware; hardware.WWN != "0x5000c500a1b2c3d4" || hardware.SerialNumber != "ZC1A2B3C" ||
		!strings.Contains(request.Kickstart, "!= 'naa.5000c500a1b2c3d4'") || !strings.Contains(request.Kickstart, "!= 'ZC1A2B3C'") {
		t.Fatalf("the root disk identity was not frozen and compared: %+v", hardware)
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-physical-host-variants", canonical)
}
