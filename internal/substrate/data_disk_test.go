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
	for _, disk := range []api.Value{ordinary.With("name", api.StringValue("root")), ordinary.With("media", api.StringValue("")), ordinary.With("media", api.BoolValue(true)), ordinary.With("media", api.StringValue("hdd"))} {
		if _, valid := DataDiskPresentations([]api.Value{disk}); valid {
			t.Fatal("unsupported data-disk media or root switch admitted")
		}
	}
	if _, valid := DataDiskPresentations(make([]api.Value, MaxDataDisks+1)); valid {
		t.Fatal("data-disk presentation exceeded original total bound")
	}
}
