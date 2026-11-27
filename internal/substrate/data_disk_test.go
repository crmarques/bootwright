package substrate

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestDataDiskPresentationsUseIndependentBoundedMediaSequences(t *testing.T) {
	ordinary := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("data")})
	ssd := ordinary.With("media", api.StringValue("ssd"))
	got, valid := DataDiskPresentations([]api.Value{ssd, ordinary, ssd, ordinary})
	want := []DataDiskPresentation{{Target: "sda", Bus: "scsi", Media: "ssd", RotationRate: 1}, {Target: "vdb"}, {Target: "sdb", Bus: "scsi", Media: "ssd", RotationRate: 1}, {Target: "vdc"}}
	if !valid || !reflect.DeepEqual(got, want) {
		t.Fatalf("presentations=%+v valid=%v", got, valid)
	}
	for _, disk := range []api.Value{ordinary.With("name", api.StringValue("root")), ordinary.With("media", api.StringValue("")), ordinary.With("media", api.BoolValue(true)), ordinary.With("media", api.StringValue("nvme"))} {
		if _, valid := DataDiskPresentations([]api.Value{disk}); valid {
			t.Fatal("unsupported data-disk media or root switch admitted")
		}
	}
	if _, valid := DataDiskPresentations(make([]api.Value, MaxDataDisks+1)); valid {
		t.Fatal("data-disk presentation exceeded original total bound")
	}
}

func TestHDDDataDiskHasKnownRotationalPresentation(t *testing.T) {
	disk := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("data")}, api.FieldValue{Name: "media", Value: api.StringValue("hdd")})
	got, valid := DataDiskPresentations([]api.Value{disk})
	if !valid || len(got) != 1 || got[0] != (DataDiskPresentation{Target: "sda", Bus: "scsi", Media: "hdd", RotationRate: 7200}) {
		t.Fatalf("HDD presentation = %+v, valid %v", got, valid)
	}
}

func TestGuestDataDisksProjectTheAssignedKernelPathAndSerial(t *testing.T) {
	guest := obj(api.Machine, "node", m("hardware", m("providerRef", "pool", "profileRef", "small")))
	data := m("name", "data", "sizeGiB", api.IntegerValue("30"))
	hdd := m("name", "rotating", "sizeGiB", api.IntegerValue("60"), "media", "hdd")
	provider := func(disks ...api.Value) api.Catalog {
		pool := obj(api.InfraProvider, "pool", m("libvirt", m("machineProfiles", list(m("name", "small", "dataDisks", list(disks...))))))
		return api.NewCatalog([]api.Object{guest, pool})
	}
	got, valid := GuestDataDisks(provider(data, hdd), guest, "lab")
	want := []GuestDataDisk{
		{Path: "/dev/vdb", Serial: DataDiskSerial("lab", "node", "data"), SizeBytes: 30 << 30},
		{Path: "/dev/sda", Serial: DataDiskSerial("lab", "node", "rotating"), SizeBytes: 60 << 30, Rotational: true},
	}
	if !valid || !reflect.DeepEqual(got, want) || VirtioDiskAlias(got[0].Serial) != "/dev/disk/by-id/virtio-"+want[0].Serial {
		t.Fatalf("guest disk projection = %v (%t), want %v", got, valid, want)
	}
	if _, valid := GuestDataDisks(provider(data.With("name", api.StringValue("root"))), guest, "lab"); valid {
		t.Fatal("an invalid data disk profile still projects guest devices")
	}
}
