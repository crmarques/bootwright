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
	presentation := map[string]bool{"version": true, "help": true}
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
		"secret list": true, "secret show": true, "secret delete": true,
		"secret encryption init": true, "secret encryption status": true,
		"secret encryption rotate": true, "validate": true, "render effective": true,
		"setup": true, "preflight controller": true,
		"plan": true, "status": true, "apply": true, "destroy": true,
	}
	for _, spec := range commandCatalog() {
		if implementedOperation(spec.path) != want[spec.path] {
			t.Errorf("%s: implemented = %t, want %t", spec.path, implementedOperation(spec.path), want[spec.path])
		}
		if privilegedOperation(spec.path) != want[spec.path] {
			t.Errorf("%s: privileged = %t, want %t", spec.path, privilegedOperation(spec.path), want[spec.path])
		}
	}
	if implementedOperation("controller") || privilegedOperation("machine list") {
		t.Fatal("an incomplete path or unavailable command claimed a mode")
	}
}
