package operationstore

import (
	"context"
	"encoding/json"
	"path"
	"reflect"
	"strings"
	"testing"
)

func testSuspension(state string) Suspension {
	return Suspension{Version: SuspensionVersion, Block: "ceph-signon-lab", Request: strings.Repeat("ab", 32), State: state,
		Preparation: json.RawMessage(`{"daemons":[]}`)}
}

func TestASuspensionRecordIsWrittenOnlyOverWhatItsStoreRead(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	id := "op-" + strings.Repeat("ab", 16)
	if err := store.PublishSuspension(ctx, id, testSuspension(SuspensionSuspending)); err == nil {
		t.Fatal("a suspension record was written with no read guard")
	}
	if _, found, err := store.ReadSuspension(ctx, id, "ceph-signon-lab"); err != nil || found {
		t.Fatalf("an absent record reads %v (%v)", found, err)
	}
	if err := store.PublishSuspension(ctx, id, testSuspension(SuspensionSuspending)); err != nil {
		t.Fatal(err)
	}
	target := path.Join(id, "suspensions", "ceph-signon-lab.json")
	want := `{"block":"ceph-signon-lab","preparation":{"daemons":[]},"request":"` + strings.Repeat("ab", 32) + `","state":"suspending","version":"sign-on-suspension-v1"}` + "\n"
	if string(area.files[target]) != want {
		t.Fatalf("the record is %s", area.files[target])
	}
	stale := New(area, area.logs, fixedClock())
	if _, _, err := stale.ReadSuspension(ctx, id, "ceph-signon-lab"); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishSuspension(ctx, id, testSuspension(SuspensionSuspended)); err != nil {
		t.Fatal(err)
	}
	if err := stale.PublishSuspension(ctx, id, testSuspension(SuspensionResuming)); err == nil {
		t.Fatal("a stale store replaced a record another writer changed")
	}
	if err := stale.ClearSuspension(ctx, id, "ceph-signon-lab"); err == nil {
		t.Fatal("a stale store removed a record another writer changed")
	}
	read, found, err := New(area, area.logs, fixedClock()).ReadSuspension(ctx, id, "ceph-signon-lab")
	if err != nil || !found || !reflect.DeepEqual(read, testSuspension(SuspensionSuspended)) {
		t.Fatalf("the record reads back %+v %v (%v)", read, found, err)
	}
	if err := store.ClearSuspension(ctx, id, "ceph-signon-lab"); err != nil {
		t.Fatal(err)
	}
	if _, present := area.files[target]; present {
		t.Fatal("a cleared record is still present")
	}
}

func TestASuspensionRecordIsClosedAndBounded(t *testing.T) {
	ctx := context.Background()
	id := "op-" + strings.Repeat("ab", 16)
	target := path.Join(id, "suspensions", "ceph-signon-lab.json")
	valid := `{"block":"ceph-signon-lab","preparation":{},"request":"` + strings.Repeat("ab", 32) + `","state":"suspended","version":"sign-on-suspension-v1"}` + "\n"
	for name, data := range map[string]string{
		"an unknown member":  strings.Replace(valid, `"block"`, `"extra":1,"block"`, 1),
		"an unknown state":   strings.Replace(valid, `"suspended"`, `"paused"`, 1),
		"another version":    strings.Replace(valid, "sign-on-suspension-v1", "sign-on-suspension-v2", 1),
		"another block":      strings.Replace(valid, "ceph-signon-lab", "ceph-signon-edge", 1),
		"a short digest":     strings.Replace(valid, strings.Repeat("ab", 32), "abcd", 1),
		"a list preparation": strings.Replace(valid, `"preparation":{}`, `"preparation":[]`, 1),
		"not canonical":      strings.Replace(valid, `"preparation":{}`, `"preparation": {}`, 1),
		"oversized":          strings.Replace(valid, `"preparation":{}`, `"preparation":{"x":"`+strings.Repeat("x", MaxSuspensionBytes)+`"}`, 1),
	} {
		_, area := newStore(t)
		area.files[target] = []byte(data)
		if _, _, err := New(area, area.logs, fixedClock()).ReadSuspension(ctx, id, "ceph-signon-lab"); err == nil {
			t.Fatalf("a record with %s reads", name)
		}
	}
	_, area := newStore(t)
	area.files[target] = []byte(valid)
	if _, found, err := New(area, area.logs, fixedClock()).ReadSuspension(ctx, id, "ceph-signon-lab"); err != nil || !found {
		t.Fatalf("a valid record reads %v (%v)", found, err)
	}
}
