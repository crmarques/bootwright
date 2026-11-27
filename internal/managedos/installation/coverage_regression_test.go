package installation

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

func TestCapacityWritesSeparateUnfinishedInstallAndSharedTree(t *testing.T) {
	catalog := labCatalog()
	request := pinnedRequest(t, catalog)
	allocation := capacity.Allocation{Machine: request.Identity.Object, Staging: "/fixture/staging", Host: "controller", Context: testContext, Local: true}
	project := CapacityWrites(func(context.Context) (map[string]int64, error) {
		return map[string]int64{request.TreeMedia.Name: 100, request.BootMedia.Name: 200}, nil
	})
	for _, tc := range []struct {
		name   string
		active string
		labels []string
		bytes  int64
	}{
		{"settled install and tree", "", nil, 0},
		{"only tree unfinished", TreeKind, []string{TreeWrite + " " + request.Identity.Profile}, 100 + TreeAllowance},
		{"only install unfinished", Kind, []string{PublishedWrite, StagedWrite}, 200 + InstallerAllowance},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen []string
			writes, err := project(context.Background(), catalog, testContext, []capacity.Allocation{allocation}, func(kind, implementation, object string) (bool, error) {
				seen = append(seen, kind+"/"+implementation+"/"+object)
				return kind == tc.active, nil
			})
			want := []string{TreeKind + "/" + TreeImplementation + "/" + request.Identity.Profile, Kind + "/" + Implementation + "/" + request.Identity.Object}
			if err != nil || !slices.Equal(seen, want) {
				t.Fatalf("capacity dependencies = %v (%v), want %v", seen, err, want)
			}
			var labels []string
			for _, write := range writes {
				labels = append(labels, write.Label)
				if write.Bytes != tc.bytes || write.Host != "controller" || !write.Local {
					t.Errorf("write = %+v, want %d bytes on the local controller", write, tc.bytes)
				}
			}
			if !slices.Equal(labels, tc.labels) {
				t.Fatalf("write labels = %v, want %v", labels, tc.labels)
			}
		})
	}
	for _, failedKind := range []string{TreeKind, Kind} {
		t.Run("cannot read "+failedKind, func(t *testing.T) {
			missing := errors.New("completion state unavailable")
			writes, err := project(context.Background(), catalog, testContext, []capacity.Allocation{allocation}, func(kind, _, _ string) (bool, error) {
				if kind == failedKind {
					return false, missing
				}
				return true, nil
			})
			if len(writes) != 0 || !errors.Is(err, missing) {
				t.Fatalf("unknown completion yielded writes %+v and error %v", writes, err)
			}
		})
	}
}
