package cli

import (
	"strings"
	"testing"
)

func TestReclaimDestructiveDescriptors(t *testing.T) {
	if got := reclaimDestructiveDescriptors("/dev/sdb", []string{"ceph"}); len(got) != 1 || !strings.Contains(got[0], "/dev/sdb") || !strings.Contains(got[0], "ceph") {
		t.Fatalf("reclaim of an owned cluster must yield a data-loss descriptor, got %v", got)
	}
	if got := reclaimDestructiveDescriptors("/dev/sdb", nil); got != nil {
		t.Fatalf("reclaim with no owned cluster must not add a data-loss descriptor, got %v", got)
	}
	if got := reclaimDestructiveDescriptors("", []string{"ceph"}); got != nil {
		t.Fatalf("no reclaim devices must not add a data-loss descriptor, got %v", got)
	}
}

func TestDestructiveOverrideYesGuard(t *testing.T) {
	objs := []string{"Machine/db1", "StorageCluster/ceph"}
	cases := []struct {
		name        string
		destructive []string
		yes         bool
		allow       bool
		wantErr     bool
	}{
		{"yes without allow refuses", objs, true, false, true},
		{"yes with allow proceeds", objs, true, true, false},
		{"interactive falls through to confirm", objs, false, false, false},
		{"no destructive objects proceeds", nil, true, false, false},
		{"allow with no yes proceeds", objs, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := destructiveOverrideYesGuard(tc.destructive, tc.yes, tc.allow)
			if tc.wantErr != (err != nil) {
				t.Fatalf("guard(destructive=%v yes=%v allow=%v) err=%v, wantErr=%v", tc.destructive, tc.yes, tc.allow, err, tc.wantErr)
			}
			if err != nil {
				for _, obj := range tc.destructive {
					if !strings.Contains(err.Error(), obj) {
						t.Fatalf("refusal must name %q: %v", obj, err)
					}
				}
				if !strings.Contains(err.Error(), "--allow-destroy") {
					t.Fatalf("refusal must point at --allow-destroy: %v", err)
				}
			}
		})
	}
}
