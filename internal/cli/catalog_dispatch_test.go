package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestEveryCatalogCommandHasApplicationDispatch proves the catalog and the
// dispatch switch agree. A command added to the public tree without its
// dispatch arm fails here instead of reaching an operator as an internal error.
func TestEveryCatalogCommandHasApplicationDispatch(t *testing.T) {
	presentation := map[string]bool{"version": true, "schema": true, "help": true}
	for _, spec := range commandCatalog() {
		if presentation[spec.path] || strings.HasPrefix(spec.path, "completion ") {
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
		"context init": true, "context update": true, "context use": true,
		"context list": true, "context current": true, "context delete": true,
		"secret set": true, "secret generate": true, "secret check": true,
		"secret list": true, "secret info": true, "secret show": true, "secret delete": true,
		"secret encryption init": true, "secret encryption status": true,
		"secret encryption rotate": true, "validate": true, "render effective": true,
		"media add": true, "media list": true, "media delete": true,
		"controller install": true, "controller rebind": true, "controller uninstall": true, "preflight controller": true, "preflight storage-cluster": true, "preflight container-cluster": true, "preflight clusters": true, "preflight infra": true, "render": true, "preflight all": true,
		"plan": true, "status": true, "diff": true, "apply": true, "destroy": true,
		"machine list": true, "machine info": true, "machine rsh": true, "machine exec": true,
		"cluster rsh": true, "cluster exec": true, "cluster list": true, "cluster info": true,
		"machine start": true, "machine stop": true, "machine restart": true, "machine trust": true,
		"cluster kubeconfig": true, "cluster keyring": true,
	}
	for _, spec := range commandCatalog() {
		if implementedOperation(spec.path) != (want[spec.path] || spec.path == "add-ons list" || spec.path == "add-ons info") {
			t.Errorf("%s: implemented = %t, want %t", spec.path, implementedOperation(spec.path), want[spec.path])
		}
		if privilegedOperation(spec.path) != want[spec.path] {
			t.Errorf("%s: privileged = %t, want %t", spec.path, privilegedOperation(spec.path), want[spec.path])
		}
	}
	if implementedOperation("controller") || privilegedOperation("cluster oc") {
		t.Fatal("an incomplete path or unavailable command claimed a mode")
	}
}
