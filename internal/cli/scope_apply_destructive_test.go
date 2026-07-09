package cli

import (
	"strings"
	"testing"
)

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
