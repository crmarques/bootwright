package lifecycle

import (
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestAnUnclaimedSMBResourceRefusesTheOperation(t *testing.T) {
	smb := api.NewObject(api.CephSMBResource, "smb-files", api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")}))
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.CephCluster, "ceph", api.Value{}, api.MapValue()), smb})
	claimed := []string{string(api.Machine)}
	for _, kind := range api.Kinds() {
		if strings.HasPrefix(string(kind), "Ceph") && kind != api.CephSMBResource {
			claimed = append(claimed, string(kind))
		}
	}
	want := []Refusal{RefusalOf(smb, "no capability of this executable realizes the CephSMBResource kind", unclaimedRemediation(smb.Identity()))}
	if got := Unrealizable(catalog, claimed, domainEffects); !reflect.DeepEqual(got, want) {
		t.Fatalf("unclaimed SMB resource refusals = %+v, want %+v", got, want)
	}
}

func TestAnUnclaimedNVMeoFSubsystemRefusesTheOperation(t *testing.T) {
	subsystem := api.NewObject(api.CephNVMeoFSubsystem, "block-a", api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")}))
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.CephCluster, "ceph", api.Value{}, api.MapValue()), subsystem})
	claimed := []string{string(api.Machine)}
	for _, kind := range api.Kinds() {
		if strings.HasPrefix(string(kind), "Ceph") && kind != api.CephNVMeoFSubsystem {
			claimed = append(claimed, string(kind))
		}
	}
	want := []Refusal{RefusalOf(subsystem, "no capability of this executable realizes the CephNVMeoFSubsystem kind", unclaimedRemediation(subsystem.Identity()))}
	if got := Unrealizable(catalog, claimed, domainEffects); !reflect.DeepEqual(got, want) {
		t.Fatalf("unclaimed NVMe-oF subsystem refusals = %+v, want %+v", got, want)
	}
}
