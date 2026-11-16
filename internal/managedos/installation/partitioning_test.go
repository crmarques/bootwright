package installation

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func withHomePartition(profile api.Object, policy string) api.Object {
	return profile.WithSpec(profile.Spec().WithPath(api.StringValue(policy), "installer", "anaconda", "partitioning", "home"))
}

func TestHomePartitionPolicyPreservesPhysicalDiskProofs(t *testing.T) {
	for _, selector := range []string{"wwn", "serial", "volume"} {
		t.Run(selector, func(t *testing.T) {
			input := physicalInstallation()
			switch selector {
			case "serial":
				input.RootDevice, input.RootSerial, input.RootSizeBytes = "", "ZC1A2B3C", 480103981056
			case "volume":
				input.RootDevice, input.RootVolume = "", true
			}
			original, err := RenderKickstart(input)
			if err != nil {
				t.Fatal(err)
			}
			input.HomePartition = "none"
			rendered, err := RenderKickstart(input)
			if err != nil || rendered != strings.ReplaceAll(original, "autopart --type=lvm", "autopart --type=lvm --nohome") {
				t.Fatalf("the home policy changed the physical target proof or disk fence: %v", err)
			}
		})
	}
}

func TestRenderingRefusesAnUnknownHomePartitionPolicy(t *testing.T) {
	for _, policy := range []string{"separate", "none --encrypted", "none\n%post\nprobe\n%end"} {
		input := physicalInstallation()
		input.HomePartition = policy
		if _, err := RenderKickstart(input); err == nil || len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "api.value" {
			t.Fatalf("an unknown policy reached Kickstart: %v", err)
		}
	}
}

func TestHomePartitionPolicyIsFrozenInPlainAndEncryptedInstallations(t *testing.T) {
	for name, profile := range map[string]api.Object{
		"plain": installProfile(), "encrypted": encryptingProfile(), "encrypted-fips": encryptingFIPSProfile(),
	} {
		t.Run(name, func(t *testing.T) {
			original := pinnedRequest(t, labCatalog(profile))
			automatic := pinnedRequest(t, labCatalog(withHomePartition(profile, "automatic")))
			none := pinnedRequest(t, labCatalog(withHomePartition(profile, "none")))
			if original.Kickstart != automatic.Kickstart {
				t.Fatal("explicit automatic partitioning changed the native default")
			}
			want := strings.ReplaceAll(original.Kickstart, "autopart --type=lvm", "autopart --type=lvm --nohome")
			if want == original.Kickstart || none.Kickstart != want {
				t.Fatal("the policy did not add only --nohome to the frozen storage directive")
			}
			before, err := original.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := none.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if string(before) == string(canonical) {
				t.Fatal("the partition policy did not change the frozen request")
			}
			decoded, err := DecodeRequest(canonical)
			if err != nil || decoded.Kickstart != none.Kickstart {
				t.Fatalf("the frozen policy did not round-trip: %v", err)
			}
		})
	}
}
