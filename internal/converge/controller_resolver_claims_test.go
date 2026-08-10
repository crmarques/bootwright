package converge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestOtherContextControllerResolverClaimBlocksDifferentService(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "spoke")
	hub := mustContext(t, "hub")
	saveRecord(t, hub.OwnershipDir, ownership.ResourceRecord{
		Kind:  string(ownership.KindControllerNameResolver),
		Name:  "interrupted-hub-route",
		Owner: ownership.Owner,
	})

	claims, warnings, err := OtherContextControllerResolverClaims("spoke")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	refusal := ControllerResolverClaimRefusal(claims, warnings, nil)
	if refusal == nil || !strings.Contains(refusal.Error(), "hub") || !strings.Contains(refusal.Error(), "interrupted-hub-route") || !strings.Contains(refusal.Error(), "no authorization token") {
		t.Fatalf("sibling controller claim refusal = %v", refusal)
	}
	var typed remedy.Error
	if !errors.As(refusal, &typed) || typed.Remedy().Action != remedy.ActionRetrySameInvocation {
		t.Fatalf("sibling controller claim remedy = %#v, want exact same-invocation retry", typed)
	}
}

func TestOtherContextControllerResolverClaimIgnoresUnrelatedRecords(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "spoke")
	hub := mustContext(t, "hub")
	saveRecord(t, hub.OwnershipDir, ownership.ResourceRecord{Kind: string(ownership.KindInfraComponent), Name: "unrelated", Owner: ownership.Owner})

	claims, warnings, err := OtherContextControllerResolverClaims("spoke")
	if err != nil || len(warnings) != 0 || len(claims) != 0 {
		t.Fatalf("unrelated sibling records produced claims=%v warnings=%v err=%v", claims, warnings, err)
	}
}

func TestOtherContextControllerResolverClaimRefusesUnsafeSiblingDirectories(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{
			name: "ownership root symlink",
			prepare: func(t *testing.T, root string) {
				if err := os.RemoveAll(root); err != nil {
					t.Fatalf("remove ownership root: %v", err)
				}
				if err := os.Symlink(t.TempDir(), root); err != nil {
					t.Fatalf("symlink ownership root: %v", err)
				}
			},
		},
		{
			name: "kind directory symlink",
			prepare: func(t *testing.T, root string) {
				base := filepath.Join(root, ownership.ResourceDirName)
				if err := os.MkdirAll(base, 0o700); err != nil {
					t.Fatalf("create resource root: %v", err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(base, string(ownership.KindControllerNameResolver))); err != nil {
					t.Fatalf("symlink controller resolver kind directory: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
			mustContext(t, "spoke")
			hub := mustContext(t, "hub")
			tt.prepare(t, hub.OwnershipDir)

			claims, warnings, err := OtherContextControllerResolverClaims("spoke")
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			refusal := ControllerResolverClaimRefusal(claims, warnings, nil)
			if refusal == nil || !strings.Contains(refusal.Error(), "hub") || !strings.Contains(refusal.Error(), "symbolic links") {
				t.Fatalf("unsafe sibling ownership directory refusal = %v", refusal)
			}
		})
	}
}
