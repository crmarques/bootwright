package installation

import (
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestADiscoveredRootIsProvedOnlyThroughTheBoundProof(t *testing.T) {
	request := pinnedRequest(t, labCatalog())
	if err := New(nil).proveDiscoveredRoot(lifecycle.Execution{}, request); err != nil {
		t.Fatalf("a request with no discovered root was refused: %v", err)
	}
	request.DiscoveryProof = &diskselection.Proof{Selected: diskselection.DiskIdentity{SerialNumber: "EXAMPLE-OS", SizeBytes: 1 << 30}}
	if err := New(nil).proveDiscoveredRoot(lifecycle.Execution{}, request); err == nil {
		t.Fatal("a discovered root installed with no bound proof")
	}
	refused := errors.New("refused")
	bound := New(nil).WithDiscoveredRootProof(func(lifecycle.Execution, Request) error { return refused })
	if err := bound.proveDiscoveredRoot(lifecycle.Execution{}, request); !errors.Is(err, refused) {
		t.Fatalf("the bound proof's refusal was lost: %v", err)
	}
	proved := New(nil).WithDiscoveredRootProof(func(lifecycle.Execution, Request) error { return nil })
	if err := proved.proveDiscoveredRoot(lifecycle.Execution{}, request); err != nil {
		t.Fatalf("a proved discovered root was refused: %v", err)
	}
}
