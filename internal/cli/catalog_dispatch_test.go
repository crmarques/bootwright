package cli

import (
	"context"
	"errors"
	"testing"
)

// TestEveryCatalogCommandHasApplicationDispatch proves the catalog and the
// dispatch switch agree. A command added to the public tree without its
// dispatch arm fails here instead of reaching an operator as an internal error.
func TestEveryCatalogCommandHasApplicationDispatch(t *testing.T) {
	for _, spec := range commandCatalog() {
		if spec.presentation {
			if spec.invoke != nil {
				t.Errorf("%s: a presentation command has no application dispatch", spec.path)
			}
			continue
		}
		if spec.invoke == nil {
			t.Errorf("%s: no invoke", spec.path)
			continue
		}
		_, err := Services{}.invoke(context.Background(), spec.path, dispatchFlags(), nil)
		if !errors.Is(err, errMissingService) {
			t.Errorf("%s: invoke returned %v; want the missing-service error proving a dispatch arm exists", spec.path, err)
		}
	}
}

// TestCommandModesComeFromTheCatalog keeps the availability and privilege
// decisions readable from one declaration per command.
func TestCommandModesComeFromTheCatalog(t *testing.T) {
	want := map[string]bool{
		"policy set": true, "policy show": true, "policy delete": true, "policy permissions": true,
		"context init": true, "context update": true, "context use": true,
		"context list": true, "context current": true, "context delete": true,
		"secret set": true, "secret generate": true, "secret check": true,
		"secret list": true, "secret info": true, "secret show": true, "secret delete": true,
		"secret encryption init": true, "secret encryption status": true,
		"secret encryption rotate": true, "validate": true, "render effective": true, "render acquisitions": true,
		"catalog media add": true, "catalog media list": true, "catalog media delete": true,
		"install": true, "rebind-host": true, "uninstall": true,
		"status": true, "logs": true, "diff": true, "apply": true, "destroy": true,
		"machine cp": true, "machine list": true, "machine info": true, "machine rsh": true, "machine exec": true,
		"cluster update-admin-creds": true, "cluster sign-on suspend": true, "cluster sign-on resume": true, "cluster rsh": true, "cluster exec": true, "cluster list": true, "cluster info": true,
		"machine discover": true, "machine enroll": true, "machine inventory": true, "machine start": true, "machine stop": true, "machine restart": true, "machine media eject": true, "machine media status": true, "machine trust": true,
		"cluster oc": true, "cluster kubectl": true, "cluster kubeconfig": true, "cluster keyring": true,
	}
	for _, spec := range commandCatalog() {
		if specFor(spec.path).implemented != (want[spec.path] || spec.path == "catalog add-ons list" || spec.path == "catalog add-ons info") {
			t.Errorf("%s: implemented = %t, want %t", spec.path, specFor(spec.path).implemented, want[spec.path])
		}
		if privilegedOperation(spec.path) != want[spec.path] {
			t.Errorf("%s: privileged = %t, want %t", spec.path, privilegedOperation(spec.path), want[spec.path])
		}
	}
	if specFor("controller").implemented || privilegedOperation("render installer") {
		t.Fatal("an incomplete path or unavailable command claimed a mode")
	}
}
