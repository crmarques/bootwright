//go:build linux && amd64

package bundlelocal

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A bootstrap object's RUNPATH is read and searched: a dependency only a
// RUNPATH directory of the private projection provides is qualified, and a
// RUNPATH that names a directory outside the projection is refused, which
// needs the network-tagged qualification no more (X33's mutants, B255).
func TestABootstrapRunpathIsReadAndHeldToThePrivateProjection(t *testing.T) {
	decoded, isELF, err := decodeBootstrapELF("/python/lib/libtool.so", syntheticSharedObject([]string{"libdep.so.1"}, "/python/old", "$ORIGIN/private"))
	if err != nil || !isELF {
		t.Fatalf("the synthetic object did not decode: %v %v", isELF, err)
	}
	if !slices.Equal(decoded.needed, []string{"libdep.so.1"}) || !slices.Equal(decoded.rpath, []string{"/python/old"}) || !slices.Equal(decoded.runpath, []string{"$ORIGIN/private"}) {
		t.Fatalf("decoded %+v", decoded)
	}
	projected := newProjection()
	if err := projected.add("python/lib/private/libdep.so.1", []byte("library"), false); err != nil {
		t.Fatal(err)
	}
	if err := qualifyPrivateDependencies(projected, decoded, map[string]bool{}); err != nil {
		t.Fatalf("a dependency its RUNPATH provides was refused: %v", err)
	}
	escaping, _, err := decodeBootstrapELF("/python/lib/libtool.so", syntheticSharedObject([]string{"libdep.so.1"}, "", "/usr/lib64"))
	if err != nil {
		t.Fatal(err)
	}
	if found := diagnostics.Of(qualifyPrivateDependencies(newProjection(), escaping, map[string]bool{})); len(found) != 1 || !strings.Contains(found[0].Message, "escapes its private projection") {
		t.Fatalf("a RUNPATH outside the projection was accepted: %+v", found)
	}
}
