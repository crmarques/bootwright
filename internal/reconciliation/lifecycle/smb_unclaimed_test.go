package lifecycle

import (
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestAnUnclaimedSMBResourceRefusesTheOperation(t *testing.T) {
	smb := api.NewObject(api.StorageSMBResource, "smb-files", api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")}))
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.StorageCluster, "ceph", api.Value{}, api.MapValue()), smb})
	claimed := []string{string(api.Machine)}
	for _, kind := range api.Kinds() {
		if strings.HasPrefix(string(kind), "Storage") && kind != api.StorageSMBResource {
			claimed = append(claimed, string(kind))
		}
	}
	want := []Refusal{RefusalOf(smb, "no capability of this executable realizes the StorageSMBResource kind", unclaimedRemediation(smb.Identity()))}
	if got := Unrealizable(catalog, claimed, domainEffects); !reflect.DeepEqual(got, want) {
		t.Fatalf("unclaimed SMB resource refusals = %+v, want %+v", got, want)
	}
}

func TestAnUnclaimedNVMeoFSubsystemRefusesTheOperation(t *testing.T) {
	subsystem := api.NewObject(api.StorageNVMeoFSubsystem, "block-a", api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")}))
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.StorageCluster, "ceph", api.Value{}, api.MapValue()), subsystem})
	claimed := []string{string(api.Machine)}
	for _, kind := range api.Kinds() {
		if strings.HasPrefix(string(kind), "Storage") && kind != api.StorageNVMeoFSubsystem {
			claimed = append(claimed, string(kind))
		}
	}
	want := []Refusal{RefusalOf(subsystem, "no capability of this executable realizes the StorageNVMeoFSubsystem kind", unclaimedRemediation(subsystem.Identity()))}
	if got := Unrealizable(catalog, claimed, domainEffects); !reflect.DeepEqual(got, want) {
		t.Fatalf("unclaimed NVMe-oF subsystem refusals = %+v, want %+v", got, want)
	}
}
