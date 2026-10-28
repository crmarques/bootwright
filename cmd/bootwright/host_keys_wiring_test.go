package main

import (
	"testing"

	"github.com/crmarques/bootwright/internal/controller/hostlinux"
)

// Production registration reads the controller's own sshd host keys through
// the read-only host inspector (B868), so a placement that reaches the
// controller by an undeclared address refuses.
func TestProductionLifecycleWiringReadsTheControllersSSHHostKeys(t *testing.T) {
	deps, release := localServiceDependencies(processDependencies{})
	defer release()
	if _, ok := deps.Lifecycle.HostKeys.(hostlinux.Inspector); !ok {
		t.Fatalf("production wired %#v as the controller's host keys", deps.Lifecycle.HostKeys)
	}
}
