package installation

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestPostNetworkProgressMatchesSelectedWork(t *testing.T) {
	for variant := range 16 {
		t.Run(fmt.Sprint(variant), func(t *testing.T) {
			request := Request{Identity: Identity{Object: "node"}}
			if variant&1 != 0 {
				request.DiskEncryption = &DiskEncryption{}
			}
			if variant&2 != 0 {
				request.InstallerArguments = []string{fipsArgument}
			}
			if variant&4 != 0 {
				request.Registration = &Registration{}
			}
			if variant&8 != 0 {
				request.Update = &Update{}
			}
			want := []string{"converge-network"}
			if variant&3 != 0 {
				want = append(want, "verify-security")
			}
			if variant&4 != 0 {
				want = append(want, "register-subscription")
			}
			if variant&8 != 0 {
				want = append(want, "update-packages")
			}
			want = append(want, "verify-completion")
			apply := groups(reconciliation.Apply, request)
			ids := groupIDs(apply)
			start := slices.Index(ids, "converge-network")
			if start < 0 || !slices.Equal(ids[start:], want) {
				t.Fatalf("post-network groups = %v, want %v", ids, want)
			}
			for _, group := range apply[start:] {
				if !slices.Equal(group.Machines, []string{"node"}) || group.Description == "" {
					t.Fatalf("group lacks its target or description: %+v", group)
				}
				if group.ID == "update-packages" && (!strings.Contains(group.Description, "restart if needed") || !strings.Contains(group.Description, "verify")) {
					t.Fatalf("update progress hides restart or verification: %q", group.Description)
				}
			}
			for _, group := range groups(reconciliation.Destroy, request) {
				if slices.Contains(want, group.ID) {
					t.Fatalf("apply progress leaked into destroy: %+v", group)
				}
			}
		})
	}
}
